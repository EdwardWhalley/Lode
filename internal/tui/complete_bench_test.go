package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func BenchmarkRefreshCompletionOnALargeScript(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("%dchars", size), func(b *testing.B) {
			m := New(&countingAdapter{}, "x")
			u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m = u.(Model)
			u, _ = m.Update(schemaMsg{gen: 0, tables: schemaFixture()})
			m = u.(Model)

			line := "SELECT id, name FROM people WHERE name = 'x' -- note\n"
			m.SetQuery(strings.Repeat(line, size/len(line)) + "SELECT ord")

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.complete, _ = m.complete.refresh(byTyping, m.editor, m.schema.tables, m.current.Dialect)
			}
			if !m.complete.active() {
				b.Fatal("the dropdown should be open for the word being typed")
			}
		})
	}
}
