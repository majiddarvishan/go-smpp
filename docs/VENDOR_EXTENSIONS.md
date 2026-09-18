# Vendor extension API

Vendor-specific commands and TLVs are added through a registry builder before a session starts. Active sessions use an immutable frozen registry snapshot.

## Standard starting point

For SMPP 3.4 use codec.NewSMPP34RegistryBuilder(codec.RegistryCompatible). For SMPP 5.0 use codec.NewSMPP50RegistryBuilder.

Register vendor codec.CommandDefinition or codec.TLVDefinition values before calling Freeze, then pass the resulting *codec.Registry through session.Config.Registry.

RegistryBuilder is safe for concurrent setup. Freeze creates one immutable snapshot and permanently prevents later mutation. Duplicate registrations fail.

Custom decoders receive a borrowed view of a structurally valid PDU body or TLV value. Copy bytes that must outlive the decode callback or receive-frame lifetime.

## Compatibility mode

codec.RegistryCompatible preserves structurally valid unknown extensions for higher-level handling. codec.RegistryStrict reports unknown commands/TLVs.

Registry mode is evaluated only after frame and TLV boundaries are valid. Vendor extensions cannot override fatal length/framing safety.

Do not use mutable global registries. Build extensions during startup and share the frozen snapshot across active sessions.
