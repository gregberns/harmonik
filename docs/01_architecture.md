There needs to be a dedicated process or integrated agent process that needs to ensure the system is being built for long term sustainability. No randomly placed scripts, docs dumped arbitrarily. 

The software architecture from a component perspective needs to be layered, and hexagonal from an integration perspective. These attributes need to be enforced through deterministic checks.

The repository structure: CI configuration, formatting rules, package manager setup, and application framework, all needs to be laid out in a way that is extremely scalable. Design for 0 to 500K LOC. 

Push logic into libraries once functionality is understood and formalized.

## Implementation Language: Go

Harmonik is written in Go. Rationale:

- **Process management story.** Harmonik's core job is launching, monitoring, and managing external processes (agents, twin binaries, verification scripts, NTM). Go's `os/exec`, signal handling, goroutine model, and standard-library tooling are well-suited to this; it is the language category the surrounding tools (NTM, kerf, adze, Kilroy) already operate in.
- **Single-binary distribution.** Self-build cycles produce new harmonik builds. A statically-linked single binary is the simplest distribution unit and the simplest thing to swap mid-build during a "pause to upgrade" control (see [docs/bootstrap.md](bootstrap.md)).
- **Strong concurrency primitives.** The orchestrator manages many concurrent workflow branches, agent processes, and event subscribers. Goroutines and channels are a natural fit.
- **Reasonable AI-assisted authorability.** Modern coding agents handle Go competently. The bootstrap phase relies on assistant-aided authorship; language choice is partly a choice about how easily the bootstrap can be built.

Trade-offs accepted: less expressive type system than some alternatives, more verbose error handling. We are prioritizing operational fit over expressiveness.
