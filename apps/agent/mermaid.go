package main

import (
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// renderMermaidBlocks finds mermaid code blocks and renders them as ASCII.
func renderMermaidBlocks(markdown string) string {
	// Match ```mermaid ... ``` blocks
	mermaidRe := regexp.MustCompile("(?s)```mermaid\\s*\n(.*?)```")

	return mermaidRe.ReplaceAllStringFunc(markdown, func(match string) string {
		// Extract the mermaid content
		inner := mermaidRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		content := strings.TrimSpace(inner[1])

		// Determine diagram type and render
		rendered := renderMermaid(content)

		// Return as a styled block
		return rendered
	})
}

// renderMermaid renders a mermaid diagram as ASCII art.
func renderMermaid(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	// Detect diagram type from first line
	firstLine := strings.TrimSpace(strings.ToLower(lines[0]))

	switch {
	case strings.HasPrefix(firstLine, "flowchart") || strings.HasPrefix(firstLine, "graph"):
		return renderFlowchart(lines)
	case strings.HasPrefix(firstLine, "sequencediagram"):
		return renderSequenceDiagram(lines)
	case strings.HasPrefix(firstLine, "classDiagram"):
		return renderClassDiagram(lines)
	case strings.HasPrefix(firstLine, "statediagram"):
		return renderStateDiagram(lines)
	default:
		// For unsupported types, return formatted source
		return renderFormattedSource(lines)
	}
}

// renderFlowchart renders a flowchart/graph as ASCII.
func renderFlowchart(lines []string) string {
	var sb strings.Builder
	header := color.New(color.FgCyan, color.Bold)
	nodeColor := color.New(color.FgGreen)
	arrowColor := color.New(color.FgYellow)
	labelColor := color.New(color.FgWhite)

	sb.WriteString("\n")
	sb.WriteString(header.Sprint("┌─ Flowchart ─────────────────────────────────────┐"))
	sb.WriteString("\n│\n")

	// Parse nodes and edges
	nodes := make(map[string]string) // id -> label
	var edges []flowEdge

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// Parse node definitions: A[Label] or A(Label) or A{Label} or A((Label))
		nodeRe := regexp.MustCompile(`(\w+)\s*[\[\(\{]+([^\]\)\}]+)[\]\)\}]+`)
		if matches := nodeRe.FindAllStringSubmatch(line, -1); matches != nil {
			for _, m := range matches {
				nodes[m[1]] = m[2]
			}
		}

		// Parse edges: A --> B or A -->|label| B or A --- B
		edgeRe := regexp.MustCompile(`(\w+)\s*(-->|---|-\.-|==>)\s*(?:\|([^|]*)\|)?\s*(\w+)`)
		if matches := edgeRe.FindAllStringSubmatch(line, -1); matches != nil {
			for _, m := range matches {
				edges = append(edges, flowEdge{
					from:  m[1],
					to:    m[4],
					arrow: m[2],
					label: m[3],
				})
			}
		}
	}

	// Render nodes
	if len(nodes) > 0 {
		sb.WriteString("│  ")
		sb.WriteString(color.HiBlackString("Nodes:"))
		sb.WriteString("\n")
		for id, label := range nodes {
			sb.WriteString("│    ")
			sb.WriteString(nodeColor.Sprintf("◉ %s", id))
			if label != "" && label != id {
				sb.WriteString(labelColor.Sprintf(" = \"%s\"", label))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("│\n")
	}

	// Render edges as ASCII flow
	if len(edges) > 0 {
		sb.WriteString("│  ")
		sb.WriteString(color.HiBlackString("Flow:"))
		sb.WriteString("\n")
		for _, e := range edges {
			fromLabel := nodes[e.from]
			if fromLabel == "" {
				fromLabel = e.from
			}
			toLabel := nodes[e.to]
			if toLabel == "" {
				toLabel = e.to
			}

			sb.WriteString("│    ")
			sb.WriteString(nodeColor.Sprintf("[%s]", fromLabel))
			sb.WriteString(arrowColor.Sprint(" ──► "))
			if e.label != "" {
				sb.WriteString(labelColor.Sprintf("(%s) ", e.label))
				sb.WriteString(arrowColor.Sprint("──► "))
			}
			sb.WriteString(nodeColor.Sprintf("[%s]", toLabel))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("│\n")
	sb.WriteString(header.Sprint("└─────────────────────────────────────────────────┘"))
	sb.WriteString("\n")

	return sb.String()
}

type flowEdge struct {
	from  string
	to    string
	arrow string
	label string
}

// renderSequenceDiagram renders a sequence diagram as ASCII.
func renderSequenceDiagram(lines []string) string {
	var sb strings.Builder
	header := color.New(color.FgCyan, color.Bold)
	actorColor := color.New(color.FgGreen)
	arrowColor := color.New(color.FgYellow)
	msgColor := color.New(color.FgWhite)
	noteColor := color.New(color.FgHiBlack, color.Italic)

	sb.WriteString("\n")
	sb.WriteString(header.Sprint("┌─ Sequence Diagram ──────────────────────────────┐"))
	sb.WriteString("\n│\n")

	// Parse participants and messages
	var participants []string
	participantSet := make(map[string]bool)
	var messages []seqMessage

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// Parse participant: participant A or actor A
		partRe := regexp.MustCompile(`(?:participant|actor)\s+(\w+)(?:\s+as\s+(.+))?`)
		if m := partRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if !participantSet[name] {
				participants = append(participants, name)
				participantSet[name] = true
			}
			continue
		}

		// Parse messages: A->>B: message or A-->>B: message
		msgRe := regexp.MustCompile(`(\w+)\s*(->>|-->>|->|-->)\s*(\w+)\s*:\s*(.+)`)
		if m := msgRe.FindStringSubmatch(line); m != nil {
			from, arrow, to, msg := m[1], m[2], m[3], m[4]

			// Auto-add participants
			if !participantSet[from] {
				participants = append(participants, from)
				participantSet[from] = true
			}
			if !participantSet[to] {
				participants = append(participants, to)
				participantSet[to] = true
			}

			isDashed := strings.Contains(arrow, "--")
			messages = append(messages, seqMessage{from: from, to: to, text: msg, dashed: isDashed})
		}

		// Parse notes
		noteRe := regexp.MustCompile(`Note\s+(?:over|left of|right of)\s+(\w+)\s*:\s*(.+)`)
		if m := noteRe.FindStringSubmatch(line); m != nil {
			messages = append(messages, seqMessage{from: m[1], text: m[2], isNote: true})
		}
	}

	// Render participants
	if len(participants) > 0 {
		sb.WriteString("│  ")
		sb.WriteString(color.HiBlackString("Participants:"))
		sb.WriteString(" ")
		for i, p := range participants {
			if i > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(actorColor.Sprintf("┃%s┃", p))
		}
		sb.WriteString("\n│\n")
	}

	// Render messages
	for _, msg := range messages {
		sb.WriteString("│  ")
		if msg.isNote {
			sb.WriteString(noteColor.Sprintf("  📝 [%s]: %s", msg.from, msg.text))
		} else {
			arrow := "───────►"
			if msg.dashed {
				arrow = "- - - -►"
			}
			sb.WriteString(actorColor.Sprintf("  %s ", msg.from))
			sb.WriteString(arrowColor.Sprint(arrow))
			sb.WriteString(actorColor.Sprintf(" %s", msg.to))
			sb.WriteString(msgColor.Sprintf(": %s", msg.text))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("│\n")
	sb.WriteString(header.Sprint("└─────────────────────────────────────────────────┘"))
	sb.WriteString("\n")

	return sb.String()
}

type seqMessage struct {
	from   string
	to     string
	text   string
	dashed bool
	isNote bool
}

// renderClassDiagram renders a class diagram as ASCII.
func renderClassDiagram(lines []string) string {
	var sb strings.Builder
	header := color.New(color.FgCyan, color.Bold)
	classColor := color.New(color.FgGreen, color.Bold)
	memberColor := color.New(color.FgWhite)
	relColor := color.New(color.FgYellow)

	sb.WriteString("\n")
	sb.WriteString(header.Sprint("┌─ Class Diagram ─────────────────────────────────┐"))
	sb.WriteString("\n│\n")

	// Parse classes and relationships
	classes := make(map[string][]string) // class -> members
	var relationships []string

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// Parse class definition: class ClassName { ... }
		classRe := regexp.MustCompile(`class\s+(\w+)`)
		if m := classRe.FindStringSubmatch(line); m != nil {
			if classes[m[1]] == nil {
				classes[m[1]] = []string{}
			}
			continue
		}

		// Parse member: ClassName : +method() or -field
		memberRe := regexp.MustCompile(`(\w+)\s*:\s*([+\-#~]?\w+.*)`)
		if m := memberRe.FindStringSubmatch(line); m != nil {
			classes[m[1]] = append(classes[m[1]], m[2])
			continue
		}

		// Parse relationships: A --|> B or A ..> B
		relRe := regexp.MustCompile(`(\w+)\s*(--|>|\.\.>|--\*|--o|--)\s*(\w+)`)
		if m := relRe.FindStringSubmatch(line); m != nil {
			relationships = append(relationships, line)
		}
	}

	// Render classes
	for name, members := range classes {
		sb.WriteString("│  ")
		sb.WriteString(classColor.Sprintf("┌─ %s ─┐", name))
		sb.WriteString("\n")
		for _, member := range members {
			sb.WriteString("│  │ ")
			sb.WriteString(memberColor.Sprint(member))
			sb.WriteString("\n")
		}
		sb.WriteString("│  └───────────┘\n")
	}

	// Render relationships
	if len(relationships) > 0 {
		sb.WriteString("│\n│  ")
		sb.WriteString(color.HiBlackString("Relationships:"))
		sb.WriteString("\n")
		for _, rel := range relationships {
			sb.WriteString("│    ")
			sb.WriteString(relColor.Sprint(rel))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("│\n")
	sb.WriteString(header.Sprint("└─────────────────────────────────────────────────┘"))
	sb.WriteString("\n")

	return sb.String()
}

// renderStateDiagram renders a state diagram as ASCII.
func renderStateDiagram(lines []string) string {
	var sb strings.Builder
	header := color.New(color.FgCyan, color.Bold)
	stateColor := color.New(color.FgGreen)
	transColor := color.New(color.FgYellow)

	sb.WriteString("\n")
	sb.WriteString(header.Sprint("┌─ State Diagram ─────────────────────────────────┐"))
	sb.WriteString("\n│\n")

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// Parse transitions: State1 --> State2 or State1 --> State2 : label
		transRe := regexp.MustCompile(`(\[?\*?\]?|\w+)\s*-->\s*(\[?\*?\]?|\w+)(?:\s*:\s*(.+))?`)
		if m := transRe.FindStringSubmatch(line); m != nil {
			from := m[1]
			to := m[2]
			label := m[3]

			// Format special states
			if from == "[*]" {
				from = "●"
			}
			if to == "[*]" {
				to = "◉"
			}

			sb.WriteString("│    ")
			sb.WriteString(stateColor.Sprintf("(%s)", from))
			sb.WriteString(transColor.Sprint(" ──► "))
			sb.WriteString(stateColor.Sprintf("(%s)", to))
			if label != "" {
				sb.WriteString(color.HiBlackString(" [%s]", label))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("│\n")
	sb.WriteString(header.Sprint("└─────────────────────────────────────────────────┘"))
	sb.WriteString("\n")

	return sb.String()
}

// renderFormattedSource renders unsupported diagram types with nice formatting.
func renderFormattedSource(lines []string) string {
	var sb strings.Builder
	header := color.New(color.FgCyan, color.Bold)
	lineColor := color.New(color.FgWhite)

	diagramType := "Diagram"
	if len(lines) > 0 {
		diagramType = strings.TrimSpace(lines[0])
	}

	sb.WriteString("\n")
	sb.WriteString(header.Sprintf("┌─ %s ─┐", diagramType))
	sb.WriteString("\n")

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sb.WriteString("│  ")
		sb.WriteString(lineColor.Sprint(line))
		sb.WriteString("\n")
	}

	sb.WriteString(header.Sprint("└────────────────────────────────────────┘"))
	sb.WriteString("\n")

	return sb.String()
}
