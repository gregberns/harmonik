# Research — T2 (L1 in-process bus/hub)

No open research questions: `daemon.NewSubscribeHub`'s injected `Now`/`NewTimer` and `net.Pipe()` are
confirmed usable seams (design doc §2 "L1", §4 "immediately available seams"). Risk: the B1 pin bead must
assert the DOCUMENTED drain-0 behavior (cursor-sharing), not attempt to "fix" it — flagged explicitly in the
mission and design doc as correct-behavior-that-reads-as-breakage. Approach: register a hub follower first,
then call `HandleCommsRecv` for the same agent in the same test, assert 0-drain + that `comms log --since`
(cursor-independent) still shows the messages.
