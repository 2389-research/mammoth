// ABOUTME: Embedded filesystem for editor templates and static assets.
// ABOUTME: Exports ContentFS for use by the unified server without runtime filesystem paths.
package editor

import "embed"

//go:embed templates/*.html templates/partials/*.html static/css/*.css static/js/*.js static/examples/*.dot
var ContentFS embed.FS
