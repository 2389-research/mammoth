// ABOUTME: Pipeline generation handler that runs the embedded meta-pipeline to produce DOT from specs.
// ABOUTME: Exports SpecState to markdown, launches tracker build, stores result in project DOT field.
package web

import _ "embed"

//go:embed pipeline_from_spec.dot
var metaPipelineDOT string
