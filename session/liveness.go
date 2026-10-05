package session

import (
	"errors"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// expireDeadline is deadlineManager's single callback for every deadline in
// this session's shared heap: per-request response timeouts and, since
// Task 2.4 (Finding P5), the three liveness checks that used to run on their
// own ticker goroutine. The three liveness slots are identified by pointer —
// they are the only *deadlineItem values ever passed this session's own
// &s.xDeadline fields — so routing to their dedicated handlers costs nothing
// beyond the comparison, and item.kind stays free for its original meaning
// (what TimeoutError.Kind a request-deadline firing should report) without
// needing new exported values.
func (s *Session) expireDeadline(item *deadlineItem) {
	if item == nil {
		return
	}
	now := time.Now()
	switch item {
	case &s.sessionInitDeadline:
		s.fireSessionInitDeadline(now)
		return
	case &s.inactivityDeadline:
		s.fireInactivityDeadline(now)
		return
	case &s.enquireLinkDeadline:
		s.fireEnquireLinkDeadline(now)
		return
	}
	err := &TimeoutError{Kind: item.kind, Command: item.command, Sequence: item.sequence, After: item.after}
	request, won := s.pending.completeError(item.sequence, err)
	if won {
		s.metrics.responseTimeouts.Add(1)
		eventKind := EventResponseTimeout
		if item.kind == TimeoutEnquireLink {
			s.metrics.enquireLinkTimeouts.Add(1)
			eventKind = EventEnquireLinkTimeout
		}
		s.emitEvent(Event{Kind: eventKind, Command: item.command, Sequence: item.sequence, Timeout: item.kind, Duration: item.after})
		s.machine.CancelOutbound(request.requestID)
	}
}

// scheduleLivenessDeadlines seeds the three liveness checks' first fire time,
// once, from New. Each check reschedules its own slot every time it fires —
// see fireSessionInitDeadline/fireInactivityDeadline/fireEnquireLinkDeadline —
// so nothing else ever calls deadlines.scheduleItemAt for these three slots
// again after this.
func (s *Session) scheduleLivenessDeadlines(now time.Time) {
	if s.config.SessionInitTimeout > 0 {
		s.deadlines.scheduleItemAt(&s.sessionInitDeadline, now, s.config.SessionInitTimeout, TimeoutSessionInit, 0, 0)
	}
	if s.config.InactivityTimeout > 0 {
		s.deadlines.scheduleItemAt(&s.inactivityDeadline, now, s.config.InactivityTimeout, TimeoutInactivity, 0, 0)
	}
	if s.config.EnquireLinkInterval > 0 {
		s.deadlines.scheduleItemAt(&s.enquireLinkDeadline, now, s.config.EnquireLinkInterval, TimeoutEnquireLink, 0, 0)
	}
}

// fireSessionInitDeadline is one-shot: SessionInitTimeout only ever matters
// before the first successful bind, and a session never returns to
// Open/Outbound afterward, so once this finds the session already bound
// there is nothing to reschedule — this slot has permanently finished its
// only job.
func (s *Session) fireSessionInitDeadline(now time.Time) {
	select {
	case <-s.done:
		return
	default:
	}
	state := s.State()
	if state != protocol.StateOpen && state != protocol.StateOutbound {
		return
	}
	s.metrics.sessionInitTimeouts.Add(1)
	s.emitEvent(Event{Kind: EventSessionInitTimeout, At: now, Timeout: TimeoutSessionInit, Duration: s.config.SessionInitTimeout})
	s.terminate(&TimeoutError{Kind: TimeoutSessionInit, After: s.config.SessionInitTimeout})
}

// fireInactivityDeadline re-derives idle from the live lastActivityTime()
// rather than trusting the time this fire was scheduled for: any activity
// recorded after scheduling (or the session not being bound yet at all) both
// mean it is not actually time to act, and the slot just reschedules itself
// for whenever it now looks like idle will genuinely reach the threshold —
// this is what lets one heap slot replace a value that used to be
// recomputed on every tick of a dedicated ticker, without waking up once per
// request the way rescheduling on every noteActivityAt call would.
func (s *Session) fireInactivityDeadline(now time.Time) {
	select {
	case <-s.done:
		return
	default:
	}
	if !isBoundState(s.State()) {
		s.deadlines.scheduleItemAt(&s.inactivityDeadline, now, s.config.InactivityTimeout, TimeoutInactivity, 0, 0)
		return
	}
	idle := now.Sub(s.lastActivityTime())
	if idle < s.config.InactivityTimeout {
		s.deadlines.scheduleItemAt(&s.inactivityDeadline, now, s.config.InactivityTimeout-idle, TimeoutInactivity, 0, 0)
		return
	}
	s.metrics.inactivityTimeouts.Add(1)
	s.emitEvent(Event{Kind: EventInactivityTimeout, At: now, Timeout: TimeoutInactivity, Duration: s.config.InactivityTimeout})
	s.terminate(&TimeoutError{Kind: TimeoutInactivity, After: s.config.InactivityTimeout})
}

// fireEnquireLinkDeadline is fireInactivityDeadline's sibling for the
// EnquireLinkInterval check, with the same re-derive-idle-and-reschedule
// shape for "not yet genuinely due". Once genuinely due, it cannot call
// tryEnquireLink synchronously the way the old dedicated livenessLoop
// goroutine safely could: tryEnquireLink blocks waiting for a response (or
// for that request's own response-timeout deadline), and that timeout lives
// on this exact same shared heap this function is running from inside of —
// deadlines.run()'s one goroutine calling expireDeadline calling this
// function. Blocking it here would stop it from ever reaching the very
// deadline that is supposed to unblock it: a self-deadlock, not merely a
// slow tick, and it doesn't recover on its own. So the probe runs in its own
// short-lived goroutine instead, and this function reschedules its slot and
// returns immediately either way, freeing deadlines.run() to keep servicing
// the heap (including that response-timeout) while the probe is in flight.
// This is a narrow, deliberate exception to "no goroutine per timeout": its
// rate is tied to idle time, not message volume — under real traffic it
// essentially never fires at all, since idle never reaches the threshold.
//
// tryEnquireLink never blocks behind window saturation — a full window only
// means application traffic is currently maximal, not that the peer has
// gone silent, so ErrWindowFull is not grounds to tear down the session. Any
// other outcome (a real timeout, a rejection, or session loss) is treated
// exactly as a blocking EnquireLink always was.
func (s *Session) fireEnquireLinkDeadline(now time.Time) {
	select {
	case <-s.done:
		return
	default:
	}
	if !isBoundState(s.State()) {
		s.deadlines.scheduleItemAt(&s.enquireLinkDeadline, now, s.config.EnquireLinkInterval, TimeoutEnquireLink, 0, 0)
		return
	}
	idle := now.Sub(s.lastActivityTime())
	if idle < s.config.EnquireLinkInterval {
		s.deadlines.scheduleItemAt(&s.enquireLinkDeadline, now, s.config.EnquireLinkInterval-idle, TimeoutEnquireLink, 0, 0)
		return
	}
	go func() {
		if err := s.tryEnquireLink(s.ctx); err != nil && !errors.Is(err, ErrWindowFull) {
			select {
			case <-s.done:
			default:
				s.terminate(err)
			}
		}
	}()
	s.deadlines.scheduleItemAt(&s.enquireLinkDeadline, now, s.config.EnquireLinkInterval, TimeoutEnquireLink, 0, 0)
}

func (s *Session) noteActivityAt(at time.Time) {
	s.lastActivity.Store(at.UnixNano())
}

func (s *Session) lastActivityTime() time.Time {
	return time.Unix(0, s.lastActivity.Load())
}
