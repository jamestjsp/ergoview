package ui

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func (m *Model) syncDetail() {
	task, ok := m.selectedTask()
	if !ok {
		m.detail.SetContent(m.styles.empty.Render("Select a task to see its details."))
		return
	}
	width := max(24, m.detail.Width()-2)
	m.detail.SetContent(m.renderTaskDetail(task, width))
	m.detail.GotoTop()
}

func (m Model) renderTaskDetail(task ergo.Task, width int) string {
	_, label, stateStyle := m.taskPresentation(task)
	var content strings.Builder
	content.WriteString(stateStyle.Render(label))
	content.WriteString("  ")
	content.WriteString(m.styles.metadata.Render(task.ID))
	content.WriteString("\n")
	content.WriteString(m.styles.brand.Render(task.Title))
	content.WriteString("\n")
	if task.ClaimedBy != "" {
		content.WriteString(m.styles.doing.Render("claimed by " + task.ClaimedBy))
		content.WriteString("\n")
	}
	if task.ParentID != "" {
		content.WriteString(m.styles.metadata.Render("container  " + m.taskLabel(task.ParentID)))
		content.WriteString("\n")
	}
	if len(task.Dependencies) > 0 {
		content.WriteString(m.styles.section.Render("Depends on"))
		content.WriteString("\n")
		for _, id := range task.Dependencies {
			content.WriteString("  ← ")
			content.WriteString(m.taskLabel(id))
			content.WriteString("\n")
		}
	}
	if len(task.Dependents) > 0 {
		content.WriteString(m.styles.section.Render("Unlocks"))
		content.WriteString("\n")
		for _, id := range task.Dependents {
			content.WriteString("  → ")
			content.WriteString(m.taskLabel(id))
			content.WriteString("\n")
		}
	}
	if task.Container {
		done, total := completedChildren(m.snapshot, task), len(task.Children)
		content.WriteString(m.styles.section.Render("Progress"))
		content.WriteString("\n")
		content.WriteString("  ")
		content.WriteString(m.styles.brand.Render(progressBar(done, total)))
		fmt.Fprintf(&content, "  %d of %d children complete\n", done, total)
	}
	if strings.TrimSpace(task.Body) != "" {
		content.WriteString(m.styles.section.Render("Description"))
		content.WriteString("\n")
		content.WriteString(m.renderMarkdown(task.Body, width))
	}
	if len(task.Messages) > 0 {
		content.WriteString(m.styles.section.Render("Activity"))
		content.WriteString("\n")
		for _, message := range task.Messages {
			content.WriteString("  ")
			content.WriteString(m.messageKindStyle(message.Kind).Render(strings.ToUpper(message.Kind)))
			content.WriteString("  ")
			content.WriteString(message.Text)
			content.WriteString("\n")
		}
	}
	if len(task.Results) > 0 {
		content.WriteString(m.styles.section.Render("Results"))
		content.WriteString("\n")
		for _, result := range task.Results {
			content.WriteString("  ↗ ")
			if result.Summary != "" && result.Summary != result.Path {
				content.WriteString(result.Summary)
				content.WriteString("  ")
			}
			content.WriteString(m.styles.metadata.Render(result.Path))
			content.WriteString("\n")
		}
	}
	return strings.TrimSpace(content.String())
}

func (m Model) taskDetailMarkdown(task ergo.Task) string {
	sections := []string{
		fmt.Sprintf("**%s**  `%s`\n\n# %s", m.taskDetailState(task), task.ID, task.Title),
	}
	if task.ClaimedBy != "" {
		sections = append(sections, "claimed by "+task.ClaimedBy)
	}
	if task.ParentID != "" {
		sections = append(sections, "container  "+m.plainTaskLabel(task.ParentID))
	}
	if len(task.Dependencies) > 0 {
		var lines strings.Builder
		lines.WriteString("## Depends on")
		for _, id := range task.Dependencies {
			lines.WriteString("\n- ← ")
			lines.WriteString(m.plainTaskLabel(id))
		}
		sections = append(sections, lines.String())
	}
	if len(task.Dependents) > 0 {
		var lines strings.Builder
		lines.WriteString("## Unlocks")
		for _, id := range task.Dependents {
			lines.WriteString("\n- → ")
			lines.WriteString(m.plainTaskLabel(id))
		}
		sections = append(sections, lines.String())
	}
	if task.Container {
		done, total := completedChildren(m.snapshot, task), len(task.Children)
		sections = append(sections, fmt.Sprintf(
			"## Progress\n\n%d of %d children complete",
			done,
			total,
		))
	}
	if strings.TrimSpace(task.Body) != "" {
		sections = append(sections, "## Description\n\n"+task.Body)
	}
	if len(task.Messages) > 0 {
		var lines strings.Builder
		lines.WriteString("## Activity")
		for _, message := range task.Messages {
			fmt.Fprintf(&lines, "\n- **%s**  %s", strings.ToUpper(message.Kind), message.Text)
		}
		sections = append(sections, lines.String())
	}
	if len(task.Results) > 0 {
		var lines strings.Builder
		lines.WriteString("## Results")
		for _, result := range task.Results {
			lines.WriteString("\n- ↗ ")
			if result.Summary != "" && result.Summary != result.Path {
				lines.WriteString(result.Summary)
				lines.WriteString("  ")
			}
			lines.WriteString(result.Path)
		}
		sections = append(sections, lines.String())
	}
	return strings.Join(sections, "\n\n")
}

func (m Model) taskDetailState(task ergo.Task) string {
	if task.Container {
		if task.Complete {
			return "DONE"
		}
		return "IN PROGRESS"
	}
	_, label, _ := m.taskPresentation(task)
	return label
}

func (m Model) messageKindStyle(kind string) lipgloss.Style {
	switch kind {
	case "done":
		return m.styles.done
	case "block":
		return m.styles.blocked
	case "cancel":
		return m.styles.canceled
	case "release":
		return m.styles.ready
	case "claim":
		return m.styles.doing
	default:
		return m.styles.metadata
	}
}

func (m Model) taskLabel(id string) string {
	task, ok := m.snapshot.Task(id)
	if !ok {
		return id
	}
	return task.Title + "  " + m.styles.metadata.Render(id)
}

func (m Model) plainTaskLabel(id string) string {
	task, ok := m.snapshot.Task(id)
	if !ok {
		return id
	}
	return task.Title + "  " + task.ID
}

func (m Model) renderMarkdown(markdown string, width int) string {
	style := "dark"
	if !m.dark {
		style = "light"
	}
	if m.noColor {
		style = "notty"
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown + "\n"
	}
	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown + "\n"
	}
	return rendered
}
