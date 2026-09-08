// Package kernel is the harmonik substrate: transport, channels, roster,
// lookup, state, and the plugin host.
//
// The kernel names no domain noun. It carries opaque byte payloads and never
// parses them, so a plugin's vocabulary never leaks into the substrate. This
// placeholder only gives the module a compile target; the substrate code lands
// in later changes. The vocabulary check keeps this file, and every file that
// joins it, free of a plugin's words.
package kernel
