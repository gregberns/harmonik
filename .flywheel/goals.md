# flywheel — operator goals file

> Read by the flywheel orchestrator at startup. Edit this file to change focus; the change takes effect on the next `harmonik supervise restart` (NOT mid-session). Keep it short (≤20 lines).

## Active epics

<!-- One bullet per active kerf work codename + a one-line "why this now". -->
- _(no active epic — orchestrator will use the unfiltered `kerf next` feed)_

## Explicit deferrals

<!-- Things to NOT pull from kerf, with the reason. -->
- _(none)_

## Pause signals

- If the file `.flywheel/PAUSE` exists, the orchestrator should drain in-flight work and halt new dispatches.
