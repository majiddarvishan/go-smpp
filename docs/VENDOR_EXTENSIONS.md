# Vendor extension API

Vendor-specific commands and TLVs are added through a registry builder before a session starts. Active sessions use an immutable frozen registry snapshot.

## Standard starting point

For SMPP 3.4 use codec.NewSMPP34RegistryBuilder(codec.RegistryCompatible). For SMPP 5.0 use codec.NewSMPP50RegistryBuilder.

Register vendor codec.CommandDefinition or codec.TLVDefinition values before calling Freeze, then pass the resulting *codec.Registry through session.Config.Registry.

## Direction and session-state enforcement for a vendor request

A standard command's direction (which role may issue it) and the session states it's valid in are enforced by the library's own operation/state matrix. A vendor request command has no entry there, so by default the library falls back to its previous, permissive rule: any bound state, either role. Set CommandDefinition.Actor (codec.ActorESME or codec.ActorSMSC; the zero value, codec.ActorAny, means no restriction) and CommandDefinition.AllowedStates ([]protocol.SessionState) to opt a specific vendor command into the same uniformly enforced check a standard command gets — a wrong-direction or wrong-state inbound request for that command is then refused with StatusInvalidBindState before your Handler ever sees it, exactly as it would be for the nearest standard equivalent. Leave AllowedStates nil (the zero value) to keep the old permissive behavior for a given vendor command; setting it is opt-in per command, not required.

Responses are unaffected by Actor/AllowedStates — only a registered request command's own inbound direction/state is checked this way.

RegistryBuilder is safe for concurrent setup. Freeze creates one immutable snapshot and permanently prevents later mutation. Duplicate registrations fail.

Custom decoders receive a borrowed view of a structurally valid PDU body or TLV value. Copy bytes that must outlive the decode callback or receive-frame lifetime.

## Compatibility mode

codec.RegistryCompatible preserves structurally valid unknown extensions for higher-level handling. codec.RegistryStrict reports unknown commands/TLVs.

Registry mode is evaluated only after frame and TLV boundaries are valid. Vendor extensions cannot override fatal length/framing safety.

Do not use mutable global registries. Build extensions during startup and share the frozen snapshot across active sessions.
