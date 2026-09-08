package web

import "embed"

//go:embed index.html app.js styles.css manifest.json sw.js icon.svg icon-192.png icon-512.png robots.txt
var Assets embed.FS
