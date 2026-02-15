// ABOUTME: Embeds web/static/ CSS and JS files for serving via the unified HTTP server.
// ABOUTME: Uses explicit subdirectory globs because //go:embed static/* does not recurse.
package web

import "embed"

//go:embed static/css/*.css static/js/*.js
var StaticFS embed.FS
