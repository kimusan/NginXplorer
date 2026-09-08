package web

import "embed"

//go:embed index.html app.js styles.css manifest.json sw.js icon.svg
var Assets embed.FS
