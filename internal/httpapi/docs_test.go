package httpapi

import (
	"html/template"
	"strings"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
)

// A page built from the tool definitions is worthless if it renders empty, and
// a template error is invisible until someone opens the page.
func TestDocsAndRecipesRender(t *testing.T) {
	app := &webApp{templates: template.Must(template.New("pages").Funcs(templateFuncs).Parse(pages))}
	for _, tc := range []struct {
		name string
		data any
		want []string
	}{
		{"documentacao", docsPage{layout: layout{Title: "Doc", Active: "documentacao"}, Tools: mcp.Catalogue(), Count: len(mcp.Catalogue())}, []string{"delete_message", "confirm", "obrigatório", "backfill_gap"}},
		{"receitas", recipesPage{layout: layout{Title: "R", Active: "receitas"}, Recipes: recipeBook()}, []string{"Agendar uma mensagem", "search_messages", "Vigiar palavras-chave"}},
	} {
		var out strings.Builder
		if err := app.templates.ExecuteTemplate(&out, tc.name, tc.data); err != nil {
			t.Fatalf("%s failed to render: %v", tc.name, err)
		}
		body := out.String()
		if len(body) < 500 {
			t.Fatalf("%s rendered only %d bytes", tc.name, len(body))
		}
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s is missing %q", tc.name, want)
			}
		}
	}
}
