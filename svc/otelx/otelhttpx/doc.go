// Package otelhttpx provides bounded HTTP instrumentation and retained
// compatibility hooks. Its transport measures one RoundTrip through response
// headers; its handler observes finite request handling, including aborts.
package otelhttpx
