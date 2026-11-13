// Command infer is go-ai's command-line client of one model endpoint. Its
// main derives go-core's signal context, so an interrupt or a SIGTERM
// cancels the run's context, and makes one call into the composition root,
// internal/app, exiting with the code that run returns: 0 on success, 1 on
// a failure, and 2 on help or a usage error.
//
// The commands are models, embed, chat, transcribe, and version; each run
// talks to the one endpoint INFER_BASE_URL or --base-url names, as
// internal/app's package comment states in full. The package exports
// nothing.
package main
