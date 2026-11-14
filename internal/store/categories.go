package store

import "github.com/pedrosatin/ttv-tui/internal/platform"

// Category guarda só a identidade da categoria, sem dados de audiência antigos.
type Category struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

// Matches compara a identidade dentro da plataforma.
func (c Category) Matches(other Category) bool {
	return platform.Normalize(c.Platform) == platform.Normalize(other.Platform) && c.ID == other.ID
}

func normalizeCategories(cats []Category) []Category {
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		if c.ID == "" || platform.HasControl(c.ID) {
			continue
		}
		if c.Name == "" {
			c.Name = c.ID
		}
		c.Platform = platform.Normalize(c.Platform)
		out = unionCategories(out, []Category{c})
	}
	return out
}

func withoutCategories(cats, excluded []Category) []Category {
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		found := false
		for _, e := range excluded {
			if c.Matches(e) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, c)
		}
	}
	return out
}

func unionCategories(first, second []Category) []Category {
	out := append([]Category(nil), first...)
	return append(out, withoutCategories(second, first)...)
}
