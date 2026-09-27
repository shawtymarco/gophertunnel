# Manual bootstrap connections

`ListenConfig.BootstrapMode` and `Dialer.BootstrapMode` default to
`BootstrapModeAutomatic`. Opting into `BootstrapModeManual` returns the
connection after authentication/encryption. The application then owns resource
pack negotiation, cache status, the complete StartGame/registry sequence,
chunk-radius negotiation and spawn acknowledgements.

Use `ReadPacket` / `WritePacket` in manual mode. They observe StartGame,
ItemRegistry (including shield ID), and DimensionData without generating
responses. LoginSuccess and post-login Disconnect remain visible to the caller.
Do not call `StartGame` or `DoSpawn` in this mode. Keep one reader per connection.
The receive queue is bounded to 4096 packets / 32 MiB and decompressed batches
are bounded to 32 MiB. Treat reaching either bound as a failed session.

`ProtocolFactory` creates a connection-local instance after an accepted protocol
is selected. It must return the same protocol ID. It cannot enable an ID absent
from `AcceptedProtocols` (except native, which is always a candidate).
`AdvertisedProtocol` / `AdvertisedVersion` only affect server-list pong data.

Regression coverage includes an encrypted RakNet loopback connection, preserving
StartGame fields and packet order, metadata observation, factory isolation,
bounded buffering, and conversions producing more than sixteen packets.
