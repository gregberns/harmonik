// Package echo is a placeholder for the echo tool: a plugin that speaks the
// wire contract to the substrate over a socket.
//
// A tool links the contract module and never the kernel module and never a
// sibling tool; it reaches the substrate and every other tool through the
// socket, not by import. The tool-isolation check holds that rule. The tool
// core, its plugin adapter, and its cmd/echo entry point land in a later
// change; this placeholder only gives the module a compile target.
package echo
