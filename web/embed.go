package web

import "embed"

//go:embed index.html app.js styles.css
var Assets embed.FS
