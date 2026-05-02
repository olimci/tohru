package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/utils/profileutils"
)

type statusRenderOptions struct {
	Flat      bool
	ColorMode string
	Stdout    *os.File
}

type trackedState struct {
	Code  string
	Label string
	Icon  string
}

type statusStyles struct {
	title       lipgloss.Style
	muted       lipgloss.Style
	node        lipgloss.Style
	ok          lipgloss.Style
	warn        lipgloss.Style
	err         lipgloss.Style
	info        lipgloss.Style
	alert       lipgloss.Style
	statusBadge map[string]lipgloss.Style
	kindBadge   lipgloss.Style
	digest      lipgloss.Style
}

type statusTreeNode struct {
	Name     string
	Tracked  *store.TrackedStatus
	Children []*statusTreeNode
	index    map[string]*statusTreeNode
}

func renderStatus(snapshot store.StatusSnapshot, opts statusRenderOptions) (string, error) {
	styles := newStatusStyles(colorEnabled(opts.ColorMode, opts.Stdout))
	var b strings.Builder

	b.WriteString(renderProfileHeader(snapshot, styles))
	b.WriteString("\n")
	b.WriteString(styles.muted.Render(renderSummary(snapshot)))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(styles.title.Render("Tracked objects:"))
	b.WriteString("\n")

	if len(snapshot.Tracked) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.muted.Render("(none)"))
		b.WriteString("\n")
		return b.String(), nil
	}

	if opts.Flat {
		for _, tracked := range snapshot.Tracked {
			b.WriteString(renderTrackedLine("  ", filepath.Base(tracked.Path), tracked, styles))
			b.WriteString("  ")
			b.WriteString(styles.muted.Render(tracked.Path))
			b.WriteString("\n")
		}
		return b.String(), nil
	}

	root := buildStatusTree(snapshot.Tracked)
	for i, child := range root.Children {
		renderTreeNode(&b, child, "", i == len(root.Children)-1, styles, true)
	}

	return b.String(), nil
}

func renderBackups(snapshot store.StatusSnapshot, opts statusRenderOptions) (string, error) {
	styles := newStatusStyles(colorEnabled(opts.ColorMode, opts.Stdout))
	var b strings.Builder

	b.WriteString(styles.title.Render("Backups referenced by state:"))
	b.WriteString("\n")
	if len(snapshot.BackupRefs) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.muted.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, ref := range snapshot.BackupRefs {
			stateLabel := "missing"
			lineStyle := styles.warn
			if ref.Present {
				stateLabel = "present"
				lineStyle = styles.ok
			}
			b.WriteString("  ")
			b.WriteString(lineStyle.Render(stateLabel))
			b.WriteString("  ")
			b.WriteString(styles.digest.Render(ref.Digest))
			b.WriteString("\n")
			for _, path := range ref.Paths {
				b.WriteString("       ")
				b.WriteString(styles.muted.Render(path))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(styles.title.Render("Unreferenced backup objects:"))
	b.WriteString("\n")
	if len(snapshot.OrphanedBackups) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.muted.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, cid := range snapshot.OrphanedBackups {
			b.WriteString("  ")
			b.WriteString(styles.warn.Render("orphan"))
			b.WriteString("  ")
			b.WriteString(styles.digest.Render(cid))
			b.WriteString("\n")
		}
	}

	if len(snapshot.BrokenBackups) > 0 {
		b.WriteString("\n")
		b.WriteString(styles.title.Render("Broken backup entries:"))
		b.WriteString("\n")
		for _, cid := range snapshot.BrokenBackups {
			b.WriteString("  ")
			b.WriteString(styles.err.Render("broken"))
			b.WriteString("  ")
			b.WriteString(styles.digest.Render(cid))
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}

func renderProfileHeader(snapshot store.StatusSnapshot, styles statusStyles) string {
	profileState := strings.ToLower(snapshot.Profile.State)
	if profileState == "loaded" && strings.TrimSpace(snapshot.Profile.Path) != "" {
		return styles.title.Render("On profile " + profileutils.DisplayName(snapshot.Profile.Slug, snapshot.Profile.Name, snapshot.Profile.Path))
	}
	return styles.title.Render("No profile loaded")
}

func renderSummary(snapshot store.StatusSnapshot) string {
	counts := map[string]int{
		"M": 0,
		"X": 0,
		"T": 0,
		"B": 0,
		"!": 0,
	}
	for _, tracked := range snapshot.Tracked {
		counts[trackedStateFor(tracked).Code]++
	}

	return fmt.Sprintf(
		"%d tracked  %d drifted  %d missing  %d new  %d backed up  %d backup-missing",
		len(snapshot.Tracked),
		counts["M"],
		counts["X"],
		counts["T"],
		counts["B"],
		counts["!"],
	)
}

func renderTreeNode(b *strings.Builder, node *statusTreeNode, prefix string, isLast bool, styles statusStyles, isRoot bool) {
	labelNode := node
	if folded := foldTreeNode(node); folded != nil {
		labelNode = folded
	}

	branch := "├"
	nextPrefix := prefix + "│ "
	if isLast {
		branch = "└"
		nextPrefix = prefix + "  "
	}
	if isRoot {
		nextPrefix = prefix
	}

	b.WriteString(prefix)
	if !isRoot {
		b.WriteString(branch)
		b.WriteString("─ ")
	}
	b.WriteString(renderTreeLabel(labelNode, styles))
	b.WriteString("\n")

	for i, child := range labelNode.Children {
		renderTreeNode(b, child, nextPrefix, i == len(labelNode.Children)-1, styles, false)
	}
}

func renderTreeLabel(node *statusTreeNode, styles statusStyles) string {
	if node.Tracked == nil {
		return styles.node.Render(node.Name)
	}
	return renderTrackedLine("", node.Name, *node.Tracked, styles)
}

func renderTrackedLine(prefix, label string, tracked store.TrackedStatus, styles statusStyles) string {
	state := trackedStateFor(tracked)
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	for _, tag := range trackedTags(tracked, state, styles) {
		parts = append(parts, tag)
	}

	parts = append(parts, trackedLineStyle(state.Code, styles).Render(formatTrackedLabel(label, tracked)))
	return strings.Join(parts, " ")
}

func trackedStateFor(tracked store.TrackedStatus) trackedState {
	switch {
	case tracked.Drifted && tracked.Missing:
		return trackedState{Code: "X", Label: "missing", Icon: "!T"}
	case tracked.Drifted:
		return trackedState{Code: "M", Label: "drifted", Icon: "?T"}
	case tracked.PrevDigest == "":
		return trackedState{Code: "T", Label: "new", Icon: "."}
	case tracked.BackupPresent:
		return trackedState{Code: "B", Label: "backed up", Icon: "+"}
	default:
		return trackedState{Code: "!", Label: "backup missing", Icon: "!B"}
	}
}

func trackedLineStyle(code string, styles statusStyles) lipgloss.Style {
	switch code {
	case "B":
		return styles.ok
	case "M":
		return styles.warn
	case "X":
		return styles.err
	case "T":
		return styles.info
	default:
		return styles.alert
	}
}

func operationIcon(operation string) string {
	switch operation {
	case "copy":
		return "C"
	case "link":
		return "L"
	default:
		return ""
	}
}

func trackedTags(tracked store.TrackedStatus, state trackedState, styles statusStyles) []string {
	tags := make([]string, 0, 3)
	if state.Icon != "" {
		tags = append(tags, styles.statusBadge[state.Code].Render(state.Icon))
	}
	if tracked.Drifted && !tracked.Missing && tracked.PrevDigest != "" && tracked.BackupPresent {
		tags = append(tags, styles.statusBadge["B"].Render("↺"))
	}
	if icon := operationIcon(tracked.Operation); icon != "" {
		tags = append(tags, styles.muted.Render(icon))
	}
	return tags
}

func formatTrackedLabel(label string, tracked store.TrackedStatus) string {
	if tracked.ManagedKind == digest.KindDir && !strings.HasSuffix(label, string(filepath.Separator)) {
		return label + string(filepath.Separator)
	}
	return label
}

func foldTreeNode(node *statusTreeNode) *statusTreeNode {
	if node == nil || node.Tracked != nil {
		return node
	}

	name := node.Name
	current := node
	for current.Tracked == nil && len(current.Children) == 1 && current.Children[0].Tracked == nil {
		current = current.Children[0]
		name = filepath.Join(name, current.Name)
	}

	if name == node.Name {
		return node
	}

	return &statusTreeNode{
		Name:     name,
		Tracked:  current.Tracked,
		Children: current.Children,
	}
}

func buildStatusTree(tracked []store.TrackedStatus) *statusTreeNode {
	root := newStatusTreeNode("")
	for i := range tracked {
		parts := statusPathParts(tracked[i].Path)
		if len(parts) == 0 {
			continue
		}

		node := root
		for _, part := range parts {
			node = node.child(part)
		}
		node.Tracked = &tracked[i]
	}
	sortStatusTree(root)
	return root
}

func newStatusTreeNode(name string) *statusTreeNode {
	return &statusTreeNode{
		Name:  name,
		index: make(map[string]*statusTreeNode),
	}
}

func (n *statusTreeNode) child(name string) *statusTreeNode {
	if child, ok := n.index[name]; ok {
		return child
	}
	child := newStatusTreeNode(name)
	n.index[name] = child
	n.Children = append(n.Children, child)
	return child
}

func sortStatusTree(node *statusTreeNode) {
	for _, child := range node.Children {
		sortStatusTree(child)
	}
	slices.SortFunc(node.Children, func(a, b *statusTreeNode) int {
		return strings.Compare(a.Name, b.Name)
	})
}

func statusPathParts(path string) []string {
	parts := fileutils.SplitPathParts(path)
	if len(parts) == 0 {
		return nil
	}

	volume := filepath.VolumeName(path)
	if volume != "" {
		parts[0] = volume + string(filepath.Separator) + parts[0]
		return parts
	}
	if filepath.IsAbs(path) {
		parts[0] = string(filepath.Separator) + parts[0]
	}
	return parts
}

func colorEnabled(mode string, stdout *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
			return false
		}
		return isTTY(stdout) && strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) != "dumb"
	case "always":
		return true
	case "never":
		return false
	default:
		return isTTY(stdout)
	}
}

func isTTY(stdout *os.File) bool {
	if stdout == nil {
		return false
	}
	info, err := stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newStatusStyles(color bool) statusStyles {
	makeStyle := func() lipgloss.Style { return lipgloss.NewStyle() }
	if !color {
		return statusStyles{
			title: makeStyle().Bold(true),
			muted: makeStyle(),
			node:  makeStyle(),
			ok:    makeStyle(),
			warn:  makeStyle(),
			err:   makeStyle(),
			info:  makeStyle(),
			alert: makeStyle(),
			statusBadge: map[string]lipgloss.Style{
				"B": makeStyle().Bold(true),
				"M": makeStyle().Bold(true),
				"X": makeStyle().Bold(true),
				"T": makeStyle().Bold(true),
				"!": makeStyle().Bold(true),
			},
			kindBadge: makeStyle(),
			digest:    makeStyle(),
		}
	}

	return statusStyles{
		title: makeStyle().Bold(true),
		muted: makeStyle().Foreground(lipgloss.Color("241")),
		node:  makeStyle().Foreground(lipgloss.Color("247")),
		ok:    makeStyle().Foreground(lipgloss.Color("42")),
		warn:  makeStyle().Foreground(lipgloss.Color("214")),
		err:   makeStyle().Foreground(lipgloss.Color("203")),
		info:  makeStyle().Foreground(lipgloss.Color("75")),
		alert: makeStyle().Foreground(lipgloss.Color("177")),
		statusBadge: map[string]lipgloss.Style{
			"B": makeStyle().Bold(true).Foreground(lipgloss.Color("42")),
			"M": makeStyle().Bold(true).Foreground(lipgloss.Color("214")),
			"X": makeStyle().Bold(true).Foreground(lipgloss.Color("203")),
			"T": makeStyle().Bold(true).Foreground(lipgloss.Color("75")),
			"!": makeStyle().Bold(true).Foreground(lipgloss.Color("177")),
		},
		kindBadge: makeStyle().Foreground(lipgloss.Color("109")),
		digest:    makeStyle().Foreground(lipgloss.Color("109")),
	}
}
