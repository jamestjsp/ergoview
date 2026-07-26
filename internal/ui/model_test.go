package ui

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func TestWideLayoutShowsOutlineAndMarkdownDetail(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model = resize(t, model, 140, 32)
	model.syncDetail()

	content := model.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "Issue tokens") {
		t.Fatalf("outline missing selected task:\n%s", plain)
	}
	if !strings.Contains(plain, "Use rotating refresh tokens.") {
		t.Fatalf("detail missing Markdown body:\n%s", plain)
	}
	assertFits(t, content, 140, 32)
}

func TestNarrowLayoutSwitchesBetweenOutlineAndDetail(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model = resize(t, model, 72, 24)
	model.syncDetail()

	outline := ansi.Strip(model.View().Content)
	if !strings.Contains(outline, "WORK") || strings.Contains(outline, "Use rotating refresh tokens.") {
		t.Fatalf("unexpected narrow outline:\n%s", outline)
	}
	updated, _ := model.Update(key("enter"))
	model = updated.(Model)
	detail := ansi.Strip(model.View().Content)
	if !strings.Contains(detail, "DETAIL") || !strings.Contains(detail, "Use rotating refresh tokens.") {
		t.Fatalf("unexpected narrow detail:\n%s", detail)
	}
	assertFits(t, model.View().Content, 72, 24)
}

func TestKeyboardNavigationAndHelp(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	initial := model.selected
	updated, _ := model.Update(key("j"))
	model = updated.(Model)
	if model.selected != initial+1 {
		t.Fatalf("selection = %d, want %d", model.selected, initial+1)
	}
	updated, _ = model.Update(key("?"))
	model = updated.(Model)
	if !strings.Contains(ansi.Strip(model.View().Content), "Ergo View keys") {
		t.Fatal("help overlay did not open")
	}
	updated, _ = model.Update(key("esc"))
	model = updated.(Model)
	if model.help {
		t.Fatal("help overlay did not close")
	}
}

func TestHelpOverlayFitsRegressionSizes(t *testing.T) {
	for _, test := range []struct {
		name   string
		view   viewMode
		width  int
		height int
	}{
		{name: "overview height 24", view: viewOverview, width: 80, height: 24},
		{name: "overview height 25", view: viewOverview, width: 80, height: 25},
		{name: "dependencies height 29", view: viewDependencies, width: 100, height: 29},
		{name: "dependencies height 30", view: viewDependencies, width: 100, height: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := resize(t, testModel(t), test.width, test.height)
			model.setView(test.view)
			updated, _ := model.Update(key("?"))
			model = updated.(Model)
			assertFits(t, model.View().Content, test.width, test.height)
		})
	}
}

func TestHelpViewportScrollsWithKeysAndMouse(t *testing.T) {
	openHelp := func() Model {
		model := resize(t, testModel(t), 80, 24)
		model.setView(viewDependencies)
		updated, _ := model.Update(key("?"))
		model = updated.(Model)
		if model.helpView.TotalLineCount() <= model.helpView.Height() {
			t.Fatal("help fixture does not overflow")
		}
		return model
	}

	for _, value := range []string{"j", "down", "pgdown"} {
		t.Run("down "+value, func(t *testing.T) {
			model := openHelp()
			updated, _ := model.Update(key(value))
			model = updated.(Model)
			if model.helpView.YOffset() == 0 {
				t.Fatalf("%s did not scroll help down", value)
			}
		})
	}
	for _, value := range []string{"k", "up", "pgup"} {
		t.Run("up "+value, func(t *testing.T) {
			model := openHelp()
			model.helpView.GotoBottom()
			offset := model.helpView.YOffset()
			updated, _ := model.Update(key(value))
			model = updated.(Model)
			if model.helpView.YOffset() >= offset {
				t.Fatalf("%s did not scroll help up", value)
			}
		})
	}

	model := openHelp()
	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.helpView.YOffset() == 0 {
		t.Fatal("mouse wheel did not scroll help down")
	}
}

func TestHelpViewportSignalsOverflowAndResets(t *testing.T) {
	model := resize(t, testModel(t), 80, 24)
	model.setView(viewDependencies)
	updated, _ := model.Update(key("?"))
	model = updated.(Model)

	if plain := ansi.Strip(model.View().Content); !strings.Contains(plain, "more below") {
		t.Fatalf("help missing lower-content signal:\n%s", plain)
	}
	model.helpView.GotoBottom()
	if plain := ansi.Strip(model.View().Content); !strings.Contains(plain, "more above") {
		t.Fatalf("help missing upper-content signal:\n%s", plain)
	}

	updated, _ = model.Update(key("esc"))
	model = updated.(Model)
	updated, _ = model.Update(key("?"))
	model = updated.(Model)
	if !model.helpView.AtTop() {
		t.Fatalf("reopened help offset = %d, want top", model.helpView.YOffset())
	}
}

func TestHelpCloseKeysAndClicksStayModal(t *testing.T) {
	for _, value := range []string{"?", "esc", "q", "enter"} {
		t.Run("close "+value, func(t *testing.T) {
			model := resize(t, testModel(t), 80, 24)
			updated, _ := model.Update(key("?"))
			model = updated.(Model)
			updated, _ = model.Update(key(value))
			model = updated.(Model)
			if model.help {
				t.Fatalf("%s did not close help", value)
			}
		})
	}

	model := resize(t, testModel(t), 120, 28)
	selected, focused := model.selected, model.focus
	updated, _ := model.Update(key("?"))
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 4, Y: 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.selected != selected || model.focus != focused {
		t.Fatalf("help click changed selection/focus to %d/%d", model.selected, model.focus)
	}
}

func TestCopySelectedReferenceWithKeyboard(t *testing.T) {
	for _, id := range []string{"TOKENS", "EPIC01"} {
		t.Run(id, func(t *testing.T) {
			model := resize(t, testModel(t), 120, 28)
			model.rebuildRows(id)

			task, ok := model.snapshot.Task(id)
			if !ok {
				t.Fatalf("task %s not found", id)
			}
			updated, command := model.Update(key("c"))
			model = updated.(Model)

			want := id + "  " + task.Title
			model, got := completeClipboardCommand(t, model, command)
			if got != want {
				t.Fatalf("clipboard content = %q, want %q", got, want)
			}
			wantStatus := "Copied " + id + " ID and title to clipboard"
			if model.status != wantStatus {
				t.Fatalf("status = %q, want %q", model.status, wantStatus)
			}
		})
	}
}

func TestCopyWritesSystemClipboardBeforeReportingSuccess(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	var written string
	model.clipboard = newNativeClipboardDestination(func(text string) error {
		written = text
		return nil
	})

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	want := model.pendingCopy.target.text
	if model.status != "" {
		t.Fatalf("status reported success before clipboard write: %q", model.status)
	}
	message, ok := command().(clipboardResultMsg)
	if !ok {
		t.Fatalf("clipboard command message type = %T", message)
	}
	if written != want {
		t.Fatalf("system clipboard received %q, want %q", written, want)
	}
	updated, fallback := model.Update(message)
	model = updated.(Model)
	if fallback != nil {
		t.Fatal("successful system clipboard write produced a fallback")
	}
	if model.status != "Copied TOKENS ID and title to clipboard" {
		t.Fatalf("status = %q", model.status)
	}
}

func TestNewUsesInjectedClipboardWriter(t *testing.T) {
	var written string
	options := testOptions(Options{})
	options.clipboard = newNativeClipboardDestination(func(text string) error {
		written = text
		return nil
	})
	model := New(testSnapshot(t), options)
	model.rebuildRows("TOKENS")

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	want := model.pendingCopy.target.text
	message, ok := command().(clipboardResultMsg)
	if !ok {
		t.Fatalf("clipboard command message type = %T", message)
	}

	if written != want {
		t.Fatalf("injected clipboard received %q, want %q", written, want)
	}
}

func TestRemoteSessionUsesTerminalClipboard(t *testing.T) {
	for _, test := range []struct {
		name  string
		focus focus
	}{
		{name: "outline reference", focus: focusOutline},
		{name: "detail export", focus: focusDetail},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := testOptions(Options{})
			options.clipboard = newTerminalClipboardDestination()
			model := New(testSnapshot(t), options)
			model.rebuildRows("TOKENS")
			model.focus = test.focus

			target, ok := model.selectedCopyTarget()
			if !ok {
				t.Fatal("copy target unavailable")
			}
			updated, command := model.Update(key("c"))
			model = updated.(Model)
			message, ok := command().(clipboardResultMsg)
			if !ok {
				t.Fatalf("clipboard command message type = %T", message)
			}
			updated, terminal := model.Update(message)
			model = updated.(Model)

			if terminal == nil {
				t.Fatal("remote copy did not produce a terminal clipboard command")
			}
			if got := fmt.Sprint(terminal()); got != target.text {
				t.Fatalf("terminal clipboard payload = %q, want %q", got, target.text)
			}
			wantStatus := copyStatus(target.subject, message.outcome)
			if model.status != wantStatus {
				t.Fatalf("status = %q, want %q", model.status, wantStatus)
			}
		})
	}
}

func TestRemoteSessionDetection(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
		want   bool
	}{
		{name: "local", values: map[string]string{}},
		{name: "connection", values: map[string]string{"SSH_CONNECTION": "client server"}, want: true},
		{name: "tty", values: map[string]string{"SSH_TTY": "/dev/pts/1"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := isRemoteSession(func(name string) string { return test.values[name] })
			if got != test.want {
				t.Fatalf("isRemoteSession() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCopySelectionWithoutClipboardIsSafe(t *testing.T) {
	model := Model{snapshot: testSnapshot(t)}
	model.rebuildRows("TOKENS")

	if command := model.copySelection(); command != nil {
		t.Fatal("model without a clipboard queue produced a copy command")
	}
}

func TestCopyFallsBackToTerminalClipboard(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	writeErr := errors.New("system clipboard unavailable")
	model.clipboard = newNativeClipboardDestination(func(string) error { return writeErr })

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	want := model.pendingCopy.target.text
	message, ok := command().(clipboardResultMsg)
	if !ok {
		t.Fatalf("clipboard command message type = %T", message)
	}
	if !errors.Is(message.outcome.err, writeErr) {
		t.Fatalf("clipboard error = %v, want %v", message.outcome.err, writeErr)
	}
	updated, fallback := model.Update(message)
	model = updated.(Model)
	if fallback == nil {
		t.Fatal("failed system clipboard write did not produce an OSC52 fallback")
	}
	if got := fmt.Sprint(fallback()); got != want {
		t.Fatalf("OSC52 fallback content = %q, want %q", got, want)
	}
	if model.status != "System clipboard unavailable; sent TOKENS ID and title via terminal clipboard" {
		t.Fatalf("status = %q", model.status)
	}
}

func TestClipboardTimeoutDegradesQueue(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var writes atomic.Int32
	destination := newNativeClipboardDestination(func(string) error {
		if writes.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	})
	timeout := make(chan time.Time, 1)
	durations := make(chan time.Duration, 1)
	var afterCalls atomic.Int32
	destination.after = func(duration time.Duration) <-chan time.Time {
		if afterCalls.Add(1) == 1 {
			durations <- duration
			return timeout
		}
		immediate := make(chan time.Time, 1)
		immediate <- time.Now()
		return immediate
	}
	model := resize(t, testModel(t), 120, 28)
	model.clipboard = destination
	model.rebuildRows("TOKENS")

	updated, firstCommand := model.Update(key("c"))
	model = updated.(Model)
	firstResult := make(chan clipboardResultMsg, 1)
	go func() {
		firstResult <- firstCommand().(clipboardResultMsg)
	}()
	<-started
	if duration := <-durations; duration != nativeClipboardTimeout {
		t.Fatalf("native clipboard timeout = %s, want %s", duration, nativeClipboardTimeout)
	}
	timeout <- time.Now()
	firstMessage := <-firstResult
	if !errors.Is(firstMessage.outcome.err, errClipboardWriteTimeout) {
		t.Fatalf("first clipboard error = %v, want timeout", firstMessage.outcome.err)
	}
	updated, fallback := model.Update(firstMessage)
	model = updated.(Model)
	if fallback == nil {
		t.Fatal("timed-out clipboard write did not produce an OSC52 fallback")
	}

	model.rebuildRows("SCHEMA")
	updated, secondCommand := model.Update(key("c"))
	model = updated.(Model)
	want := model.pendingCopy.target.text
	secondMessage := secondCommand().(clipboardResultMsg)
	if !errors.Is(secondMessage.outcome.err, errClipboardDegraded) {
		t.Fatalf("second clipboard error = %v, want degraded queue", secondMessage.outcome.err)
	}
	updated, fallback = model.Update(secondMessage)
	model = updated.(Model)
	if fallback == nil {
		t.Fatal("degraded clipboard queue did not produce an OSC52 fallback")
	}
	if got := fmt.Sprint(fallback()); got != want {
		t.Fatalf("degraded fallback payload = %q, want %q", got, want)
	}
	if got := writes.Load(); got != 1 {
		t.Fatalf("native clipboard writes = %d, want 1", got)
	}
}

func TestLargeTerminalClipboardFallbackWarns(t *testing.T) {
	model := testModel(t)
	writeErr := errors.New("system clipboard unavailable")
	model.clipboard = newNativeClipboardDestination(func(string) error { return writeErr })
	target := copyTarget{
		text:    strings.Repeat("x", osc52WarningThreshold+1),
		subject: "TOKENS detail",
	}
	request := model.clipboard.copy(target.text)
	model.pendingCopy = pendingCopy{
		target:      target,
		request:     request.id,
		interaction: model.interaction,
	}

	message := request.command().(clipboardResultMsg)
	updated, fallback := model.Update(message)
	model = updated.(Model)

	if fallback == nil {
		t.Fatal("large failed system clipboard write did not produce an OSC52 fallback")
	}
	if !strings.Contains(model.status, "3.0 KB") {
		t.Fatalf("status does not name payload size: %q", model.status)
	}
	if !strings.Contains(model.status, "large payloads may truncate") {
		t.Fatalf("status does not warn about truncation: %q", model.status)
	}
	if !strings.Contains(model.status, target.subject) {
		t.Fatalf("status does not name copied subject: %q", model.status)
	}
}

func TestCopyStatusIncludesSubjectForEveryOutcome(t *testing.T) {
	const subject = "SNOWNB detail"
	for _, test := range []struct {
		name    string
		outcome clipboardOutcome
		want    string
	}{
		{
			name:    "native",
			outcome: clipboardOutcome{channel: clipboardNative, size: 5000},
			want:    "Copied SNOWNB detail to clipboard",
		},
		{
			name:    "terminal small",
			outcome: clipboardOutcome{channel: clipboardTerminal, size: 100},
			want:    "Sent SNOWNB detail via terminal clipboard",
		},
		{
			name:    "terminal large",
			outcome: clipboardOutcome{channel: clipboardTerminal, size: 5000},
			want:    "Sent SNOWNB detail (4.9 KB) via terminal clipboard — large payloads may truncate",
		},
		{
			name:    "fallback small",
			outcome: clipboardOutcome{channel: clipboardFallback, size: 100},
			want:    "System clipboard unavailable; sent SNOWNB detail via terminal clipboard",
		},
		{
			name:    "fallback large",
			outcome: clipboardOutcome{channel: clipboardFallback, size: 5000},
			want:    "System clipboard unavailable; sent SNOWNB detail (4.9 KB) via terminal clipboard — large payloads may truncate",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := copyStatus(subject, test.outcome); got != test.want {
				t.Fatalf("copyStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCopyStatusZeroValueIsSilent(t *testing.T) {
	if got := copyStatus("TOKENS detail", clipboardOutcome{}); got != "" {
		t.Fatalf("copyStatus() = %q, want no status for an unknown outcome", got)
	}
}

func TestClipboardCompletionAfterAnotherInteractionIsDropped(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	updated, _ = model.Update(key("j"))
	model = updated.(Model)
	message := command().(clipboardResultMsg)
	updated, fallback := model.Update(message)
	model = updated.(Model)

	if fallback != nil {
		t.Fatal("late clipboard completion produced a fallback")
	}
	if model.status != "" {
		t.Fatalf("late clipboard completion set status %q", model.status)
	}
}

func TestDetailScrollKeepsCopyConfirmationRelevant(t *testing.T) {
	model := resize(t, testModel(t), 80, 24)
	model.rebuildRows("TOKENS")
	model.focus = focusDetail

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	message := command().(clipboardResultMsg)
	updated, fallback := model.Update(message)
	model = updated.(Model)

	if fallback != nil {
		t.Fatal("detail scroll caused a successful clipboard write to fall back")
	}
	const want = "Copied TOKENS detail to clipboard"
	if model.status != want {
		t.Fatalf("status after pending detail copy = %q, want %q", model.status, want)
	}

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.status != want {
		t.Fatalf("status after completed detail copy = %q, want %q", model.status, want)
	}
}

func TestOutlineScrollDismissesCopyConfirmation(t *testing.T) {
	model := resize(t, testModel(t), 80, 24)
	model.rebuildRows("TOKENS")

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	message := command().(clipboardResultMsg)
	updated, fallback := model.Update(message)
	model = updated.(Model)

	if fallback != nil {
		t.Fatal("outline scroll caused a successful clipboard write to fall back")
	}
	if model.status != "" {
		t.Fatalf("pending copy status survived outline selection change: %q", model.status)
	}

	updated, command = model.Update(key("c"))
	model = updated.(Model)
	message = command().(clipboardResultMsg)
	updated, _ = model.Update(message)
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.status != "" {
		t.Fatalf("completed copy status survived outline selection change: %q", model.status)
	}
}

func TestTerminalClipboardDeliverySurvivesAnotherInteraction(t *testing.T) {
	options := testOptions(Options{})
	options.clipboard = newTerminalClipboardDestination()
	model := resize(t, New(testSnapshot(t), options), 120, 28)
	model.rebuildRows("TOKENS")

	updated, command := model.Update(key("c"))
	model = updated.(Model)
	want := model.pendingCopy.target.text
	updated, _ = model.Update(key("j"))
	model = updated.(Model)
	message := command().(clipboardResultMsg)
	updated, terminal := model.Update(message)
	model = updated.(Model)

	if terminal == nil {
		t.Fatal("late terminal result dropped the clipboard delivery command")
	}
	if got := fmt.Sprint(terminal()); got != want {
		t.Fatalf("terminal clipboard payload = %q, want %q", got, want)
	}
	if model.status != "" {
		t.Fatalf("late terminal result set status %q", model.status)
	}
}

func TestLatestCopyResultWinsWhenEarlierResultIsAlreadyQueued(t *testing.T) {
	options := testOptions(Options{})
	options.clipboard = newTerminalClipboardDestination()
	model := resize(t, New(testSnapshot(t), options), 120, 28)

	model.rebuildRows("TOKENS")
	updated, firstCommand := model.Update(key("c"))
	model = updated.(Model)
	older := firstCommand().(clipboardResultMsg)

	model.rebuildRows("SCHEMA")
	updated, secondCommand := model.Update(key("c"))
	model = updated.(Model)
	newer := secondCommand().(clipboardResultMsg)

	updated, terminal := model.Update(older)
	model = updated.(Model)
	if terminal != nil {
		t.Fatal("queued stale result produced a terminal clipboard command")
	}
	if model.pendingCopy.request != newer.request {
		t.Fatal("queued stale result consumed the latest pending copy")
	}
	if model.status != "" {
		t.Fatalf("queued stale result set status %q", model.status)
	}

	updated, terminal = model.Update(newer)
	model = updated.(Model)
	if terminal == nil {
		t.Fatal("latest result did not produce a terminal clipboard command")
	}
	task, ok := model.snapshot.Task("SCHEMA")
	if !ok {
		t.Fatal("SCHEMA not found")
	}
	want := taskReference(task)
	if got := fmt.Sprint(terminal()); got != want {
		t.Fatalf("terminal clipboard payload = %q, want %q", got, want)
	}
	if model.status != "Sent SCHEMA ID and title via terminal clipboard" {
		t.Fatalf("status = %q", model.status)
	}
}

func TestLatestCopyRequestWins(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	var writes []string
	model.clipboard = newNativeClipboardDestination(func(text string) error {
		writes = append(writes, text)
		return nil
	})

	model.rebuildRows("TOKENS")
	updated, firstCommand := model.Update(key("c"))
	model = updated.(Model)
	model.rebuildRows("SCHEMA")
	updated, secondCommand := model.Update(key("c"))
	model = updated.(Model)
	newer := secondCommand().(clipboardResultMsg)
	older := firstCommand()
	if _, ok := older.(clipboardIgnoredMsg); !ok {
		t.Fatalf("stale clipboard message type = %T", older)
	}

	updated, fallback := model.Update(newer)
	model = updated.(Model)
	if fallback != nil {
		t.Fatal("latest clipboard write produced a fallback")
	}
	updated, fallback = model.Update(older)
	model = updated.(Model)
	if fallback != nil {
		t.Fatal("stale clipboard completion produced a fallback")
	}

	schema, ok := model.snapshot.Task("SCHEMA")
	if !ok {
		t.Fatal("SCHEMA not found")
	}
	want := taskReference(schema)
	if len(writes) != 1 || writes[0] != want {
		t.Fatalf("clipboard writes = %q, want only latest payload %q", writes, want)
	}
	if model.status != "Copied SCHEMA ID and title to clipboard" {
		t.Fatalf("status = %q", model.status)
	}
}

func TestCopySelectionMatchesViewAndFocus(t *testing.T) {
	for _, test := range []struct {
		name       string
		id         string
		view       viewMode
		focus      focus
		wantDetail bool
		wantLabel  string
		wantStatus string
	}{
		{
			name:       "overview outline copies ID and title",
			id:         "TOKENS",
			view:       viewOverview,
			focus:      focusOutline,
			wantLabel:  "copy ref",
			wantStatus: "Copied TOKENS ID and title to clipboard",
		},
		{
			name:       "overview detail copies whole detail",
			id:         "TOKENS",
			view:       viewOverview,
			focus:      focusDetail,
			wantDetail: true,
			wantLabel:  "copy detail",
			wantStatus: "Copied TOKENS detail to clipboard",
		},
		{
			name:       "empty detail body still copies detail",
			id:         "SCHEMA",
			view:       viewOverview,
			focus:      focusDetail,
			wantDetail: true,
			wantLabel:  "copy detail",
			wantStatus: "Copied SCHEMA detail to clipboard",
		},
		{
			name:       "board ignores stale detail focus",
			id:         "TOKENS",
			view:       viewBoard,
			focus:      focusDetail,
			wantLabel:  "copy ref",
			wantStatus: "Copied TOKENS ID and title to clipboard",
		},
		{
			name:       "dependencies ignore stale detail focus",
			id:         "TOKENS",
			view:       viewDependencies,
			focus:      focusDetail,
			wantLabel:  "copy ref",
			wantStatus: "Copied TOKENS ID and title to clipboard",
		},
		{
			name:       "container detail copies whole detail",
			id:         "EPIC01",
			view:       viewOverview,
			focus:      focusDetail,
			wantDetail: true,
			wantLabel:  "copy detail",
			wantStatus: "Copied EPIC01 detail to clipboard",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := testModel(t)
			model.rebuildRows(test.id)
			model.setView(test.view)
			model.focus = test.focus
			model = resize(t, model, 120, 28)

			task, ok := model.snapshot.Task(test.id)
			if !ok {
				t.Fatalf("task %s not found", test.id)
			}
			want := taskReference(task)
			if test.wantDetail {
				want = model.taskDetailMarkdown(task)
			}
			if footer := footerText(model); !strings.Contains(footer, test.wantLabel) {
				t.Fatalf("footer missing %q: %q", test.wantLabel, footer)
			}
			updated, command := model.Update(key("c"))
			model = updated.(Model)

			model, got := completeClipboardCommand(t, model, command)
			if got != want {
				t.Fatalf("clipboard content = %q, want %q", got, want)
			}
			if model.status != test.wantStatus {
				t.Fatalf("status = %q, want %q", model.status, test.wantStatus)
			}
		})
	}
}

func TestCopyDetailFromFooterMatchesKeyboard(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model.focus = focusDetail
	model = resize(t, model, 120, 28)

	keyboardUpdated, keyboardCommand := model.Update(key("c"))
	keyboardModel := keyboardUpdated.(Model)
	_, keyboardText := completeClipboardCommand(t, keyboardModel, keyboardCommand)
	updated, command := model.Update(tea.MouseClickMsg{
		X:      copyControlX(t, model),
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	model, footerText := completeClipboardCommand(t, model, command)
	if footerText != keyboardText {
		t.Fatalf("footer clipboard content differs from keyboard:\nfooter: %q\nkeyboard: %q", footerText, keyboardText)
	}
	if model.status != "Copied TOKENS detail to clipboard" {
		t.Fatalf("status = %q", model.status)
	}
}

func TestCopySelectedIDFooterWorksAcrossViewsAndWidths(t *testing.T) {
	for _, test := range []struct {
		name   string
		view   viewMode
		width  int
		height int
	}{
		{name: "narrow overview", view: viewOverview, width: 72, height: 24},
		{name: "board", view: viewBoard, width: 120, height: 30},
		{name: "dependencies", view: viewDependencies, width: 120, height: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := testModel(t)
			model.rebuildRows("TOKENS")
			model.setView(test.view)
			model = resize(t, model, test.width, test.height)

			updated, command := model.Update(tea.MouseClickMsg{
				X:      copyControlX(t, model),
				Y:      model.height - 1,
				Button: tea.MouseLeft,
			})
			model = updated.(Model)

			task, ok := model.snapshot.Task("TOKENS")
			if !ok {
				t.Fatal("task TOKENS not found")
			}
			want := taskReference(task)
			model, got := completeClipboardCommand(t, model, command)
			if got != want {
				t.Fatalf("clipboard content = %q, want %q", got, want)
			}
			if model.status != "Copied TOKENS ID and title to clipboard" {
				t.Fatalf("status = %q", model.status)
			}
		})
	}
}

func TestCopyControlIsUnavailableWithoutSelection(t *testing.T) {
	model := New(ergo.Snapshot{Root: t.TempDir()}, testOptions(Options{NoColor: true}))
	model = resize(t, model, 72, 24)

	if strings.Contains(ansi.Strip(model.View().Content), "c copy") {
		t.Fatal("empty snapshot rendered a copy control")
	}
	updated, command := model.Update(key("c"))
	model = updated.(Model)
	if command != nil {
		t.Fatal("empty snapshot produced a clipboard command")
	}
	if model.status != "" {
		t.Fatalf("empty snapshot status = %q", model.status)
	}
}

func TestCopyShortcutDoesNotInterceptSearchInput(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	updated, _ := model.Update(key("/"))
	model = updated.(Model)

	updated, _ = model.Update(key("c"))
	model = updated.(Model)

	if got := model.search.Value(); got != "c" {
		t.Fatalf("search value = %q, want c", got)
	}
	if strings.Contains(model.status, "Copied") {
		t.Fatalf("copy status set while searching: %q", model.status)
	}
}

func TestCopyFooterDoesNotBypassActionMenu(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	copyX := copyControlX(t, model)
	model.actionMenu = true

	updated, command := model.Update(tea.MouseClickMsg{
		X:      copyX,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	if command != nil {
		t.Fatal("action menu footer click produced a clipboard command")
	}
	if strings.Contains(model.status, "Copied") {
		t.Fatalf("copy status set behind action menu: %q", model.status)
	}
}

func TestCopyFooterDoesNotBypassDialog(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	copyX := copyControlX(t, model)
	model.openDialog(actionRename)

	updated, command := model.Update(tea.MouseClickMsg{
		X:      copyX,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	if command != nil {
		t.Fatal("dialog footer click produced a clipboard command")
	}
	if strings.Contains(model.status, "Copied") {
		t.Fatalf("copy status set behind dialog: %q", model.status)
	}
}

func TestFooterRowClaimsEveryMouseClick(t *testing.T) {
	for _, test := range []struct {
		name   string
		view   viewMode
		width  int
		height int
	}{
		{name: "narrow overview", view: viewOverview, width: 72, height: 12},
		{name: "wide overview", view: viewOverview, width: 120, height: 28},
		{name: "board", view: viewBoard, width: 120, height: 28},
		{name: "dependencies", view: viewDependencies, width: 120, height: 28},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, true)
			model.rebuildRows("DEDIT1")
			model.setView(test.view)
			model = resize(t, model, test.width, test.height)
			model.status = "stale"
			selected, focused := model.selected, model.focus

			updated, command := model.Update(tea.MouseClickMsg{
				X:      model.width - 2,
				Y:      model.height - 1,
				Button: tea.MouseLeft,
			})
			model = updated.(Model)

			if command != nil {
				t.Fatal("footer miss produced a command")
			}
			if model.selected != selected {
				t.Fatalf("footer miss changed selection from %d to %d", selected, model.selected)
			}
			if model.focus != focused {
				t.Fatalf("footer miss changed focus from %d to %d", focused, model.focus)
			}
			if model.status != "" {
				t.Fatalf("footer miss left status %q", model.status)
			}
		})
	}
}

func TestCopyFooterHitBoundsAreHalfOpen(t *testing.T) {
	newModel := func() Model {
		model := resize(t, testModel(t), 120, 28)
		model.rebuildRows("TOKENS")
		return model
	}
	placement := footerPlacementForAction(t, newModel(), footerActionCopy)

	model := newModel()
	updated, command := model.Update(tea.MouseClickMsg{
		X:      placement.start,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	model, got := completeClipboardCommand(t, model, command)
	if !strings.HasPrefix(got, "TOKENS") {
		t.Fatalf("start-bound clipboard content = %q, want a TOKENS reference", got)
	}

	model = newModel()
	model.status = "stale"
	updated, command = model.Update(tea.MouseClickMsg{
		X:      placement.end,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command != nil {
		t.Fatal("end-bound click produced a clipboard command")
	}
	if model.status != "" {
		t.Fatalf("end-bound click left status %q", model.status)
	}
}

func TestMouseClickClearsStatusAboveFooter(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.status = "stale"

	updated, _ := model.Update(tea.MouseClickMsg{
		X:      4,
		Y:      4,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	if model.status != "" {
		t.Fatalf("mouse click left status %q", model.status)
	}
}

func TestBoardKeepsWaitingSeparateFromBlocked(t *testing.T) {
	model := resize(t, testModel(t), 140, 32)
	model.setView(viewBoard)
	groups := model.boardGroups()
	if len(groups[1].tasks) != 1 || groups[1].tasks[0].ID != "DOCS01" {
		t.Fatalf("waiting group = %#v", groups[1].tasks)
	}
	if len(groups[3].tasks) != 1 || groups[3].tasks[0].ID != "BLOCKD" {
		t.Fatalf("blocked group = %#v", groups[3].tasks)
	}
	content := model.View().Content
	plain := ansi.Strip(content)
	for _, heading := range []string{"READY", "WAITING", "DOING", "BLOCKED", "DONE", "CANCELED"} {
		if !strings.Contains(plain, heading) {
			t.Fatalf("board missing %s:\n%s", heading, plain)
		}
	}
	assertFits(t, content, 140, 32)
}

func TestSearchMatchesBodyAndPreservesSelection(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	updated, _ := model.Update(key("/"))
	model = updated.(Model)
	for _, character := range "rotating" {
		updated, _ = model.Update(key(string(character)))
		model = updated.(Model)
	}
	if selected := model.selectedID(); selected != "TOKENS" {
		t.Fatalf("selected = %q, want TOKENS", selected)
	}
	if len(model.rows) != 2 || model.rows[0].id != "EPIC01" || model.rows[1].id != "TOKENS" {
		t.Fatalf("filtered rows = %#v", model.rows)
	}
}

func TestStateAndContainerFiltersCompose(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model.filter = filterReady
	model.containerFilter = "EPIC01"
	model.rebuildRows("TOKENS")
	if len(model.rows) != 2 || model.rows[0].id != "EPIC01" || model.rows[1].id != "TOKENS" {
		t.Fatalf("filtered rows = %#v", model.rows)
	}
	model.clearFilters()
	if model.filter != filterAll || model.containerFilter != "" || len(model.rows) != 6 {
		t.Fatalf("clear filters left filter=%v container=%q rows=%d", model.filter, model.containerFilter, len(model.rows))
	}
}

func TestViewsShareSelectionAndFitResponsiveLayouts(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 72, height: 24},
		{width: 140, height: 32},
	} {
		model := resize(t, testModel(t), size.width, size.height)
		model.rebuildRows("TOKENS")
		for _, view := range []viewMode{viewBoard, viewDependencies, viewOverview} {
			model.setView(view)
			if selected := model.selectedID(); selected != "TOKENS" {
				t.Fatalf("%dx%d view %d selected = %q", size.width, size.height, view, selected)
			}
			assertFits(t, model.View().Content, size.width, size.height)
		}
	}
}

func TestLiveReloadPreservesExistingSelection(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	updatedSnapshot := testSnapshot(t)
	updatedSnapshot.Version = "external-change"
	updated, _ := model.Update(snapshotLoadedMsg{snapshot: updatedSnapshot, changed: true})
	model = updated.(Model)
	if selected := model.selectedID(); selected != "TOKENS" {
		t.Fatalf("selected = %q, want TOKENS", selected)
	}
	if model.status == "" {
		t.Fatal("reload status was not set")
	}
}

func TestLiveReloadSkipsUnchangedSnapshot(t *testing.T) {
	snapshot := testSnapshot(t)
	source := &stubSnapshotSource{snapshot: snapshot}
	message := loadSnapshot(source)().(snapshotLoadedMsg)
	if source.calls != 1 {
		t.Fatalf("conditional loads = %d, want 1", source.calls)
	}
	if message.changed {
		t.Fatal("unchanged source reported a changed snapshot")
	}

	model := New(snapshot, testOptions(Options{Source: source}))
	model.status = "keep"
	updated, _ := model.Update(message)
	model = updated.(Model)
	if model.status != "keep" {
		t.Fatalf("unchanged reload replaced status with %q", model.status)
	}
}

func TestFuzzyMatchSupportsUnicode(t *testing.T) {
	task := ergo.Task{Title: "Crème brûlée"}
	if !matchesQuery(task, "cbr") || !matchesQuery(task, "brû") {
		t.Fatal("unicode fuzzy match failed")
	}
}

func TestNoColorRemovesANSIColorSequences(t *testing.T) {
	snapshot := testSnapshot(t)
	model := New(snapshot, testOptions(Options{NoColor: true}))
	model = resize(t, model, 120, 28)
	content := model.View().Content
	if strings.Contains(content, "\x1b[38;") || strings.Contains(content, "\x1b[48;") {
		t.Fatalf("NO_COLOR view contains ANSI color sequences:\n%q", content)
	}
}

func TestTerminalBackgroundSelectsThemePalette(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	model := testModel(t)
	updated, _ := model.Update(tea.BackgroundColorMsg{Color: color.White})
	model = updated.(Model)
	if model.dark {
		t.Fatal("white terminal background did not select light theme")
	}
}

type stubSnapshotSource struct {
	snapshot ergo.Snapshot
	changed  bool
	err      error
	calls    int
}

func (source *stubSnapshotSource) LoadIfChanged() (ergo.Snapshot, bool, error) {
	source.calls++
	return source.snapshot, source.changed, source.err
}

func testModel(t *testing.T) Model {
	t.Helper()
	return New(testSnapshot(t), testOptions(Options{}))
}

func testOptions(options Options) Options {
	options.clipboard = newNativeClipboardDestination(func(string) error { return nil })
	return options
}

func testSnapshot(t *testing.T) ergo.Snapshot {
	t.Helper()
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "ergo", "testdata", "current.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ergoDir, "plans.jsonl"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := ergo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func resize(t *testing.T, model Model, width, height int) Model {
	t.Helper()
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func key(value string) tea.KeyPressMsg {
	switch value {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "pgup":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	default:
		return tea.KeyPressMsg(tea.Key{Code: []rune(value)[0], Text: value})
	}
}

func completeClipboardCommand(t *testing.T, model Model, command tea.Cmd) (Model, string) {
	t.Helper()
	if command == nil {
		t.Fatal("clipboard command is nil")
	}
	text := model.pendingCopy.target.text
	raw := command()
	message, ok := raw.(clipboardResultMsg)
	if !ok {
		t.Fatalf("clipboard command message type = %T", raw)
	}
	if message.outcome.err != nil {
		t.Fatalf("clipboard command failed: %v", message.outcome.err)
	}
	updated, fallback := model.Update(message)
	if fallback != nil {
		t.Fatal("successful clipboard write produced a fallback command")
	}
	return updated.(Model), text
}

func copyControlX(t *testing.T, model Model) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	footer := lines[len(lines)-1]
	index := strings.Index(footer, "c copy ")
	if index < 0 {
		t.Fatalf("copy control not visible in footer:\n%s", ansi.Strip(model.View().Content))
	}
	return lipgloss.Width(footer[:index])
}

func footerPlacementForAction(t *testing.T, model Model, action footerAction) footerPlacement {
	t.Helper()
	for _, placement := range model.footerPlacements(model.footerItems()) {
		if placement.item.action == action {
			return placement
		}
	}
	t.Fatalf("footer action %d not visible:\n%s", action, ansi.Strip(model.View().Content))
	return footerPlacement{}
}

func assertFits(t *testing.T, content string, width, height int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		t.Fatalf("rendered height = %d, want <= %d:\n%s", len(lines), height, ansi.Strip(content))
	}
	for index, line := range lines {
		if lineWidth := lipgloss.Width(line); lineWidth > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", index+1, lineWidth, width, ansi.Strip(line))
		}
	}
}
