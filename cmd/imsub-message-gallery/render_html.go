package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
)

func renderHTML(page galleryPage) ([]byte, error) {
	tmpl, err := template.New("gallery").Funcs(template.FuncMap{
		//nolint:gosec // Gallery message HTML is produced by trusted internal view builders.
		"renderHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"joinLangs": func(langs []string) string {
			return strings.Join(langs, ", ")
		},
	}).Parse(htmlTemplates)
	if err != nil {
		return nil, fmt.Errorf("parse HTML templates: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page", page); err != nil {
		return nil, fmt.Errorf("execute HTML templates: %w", err)
	}
	return buf.Bytes(), nil
}
