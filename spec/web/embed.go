// ABOUTME: Embedded filesystem for spec builder templates and static assets.
// ABOUTME: Exports ContentFS for use by the unified server without runtime filesystem paths.
package web

import "embed"

//go:embed templates/* static/*
var ContentFS embed.FS
