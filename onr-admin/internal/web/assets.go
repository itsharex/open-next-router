package web

import _ "embed"

//go:embed index.html
var indexHTML string

//go:embed app.css
var appCSS string

//go:embed app.js
var appJS string
