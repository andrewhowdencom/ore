package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/tool/truncate"
)

// frameworkDefaultByteCap is the default per-call byte cap for
// the read_skill tool. Skills can be long; the cap mirrors the
// framework's default.
const frameworkDefaultByteCap = 50_000

// Toolkit binds a Catalog to a tool.ToolFunc implementation that an LLM
// can invoke to load skill instructions on demand.
type Toolkit struct {
	catalog *Catalog
}

// NewToolkit creates a Toolkit backed by a Catalog composed from the given
// discoverers.
func NewToolkit(discoverers ...Discoverer) *Toolkit {
	return &Toolkit{
		catalog: NewCatalog(discoverers...),
	}
}

// ReadSkillResult carries the bounded content of a read_skill
// call, plus optional truncation metadata and a temp file path
// that holds the full skill content when truncation occurred.
type ReadSkillResult struct {
	Content      string
	TempFilePath string
	Truncation   *artifact.Truncation
}

// MarshalLLM returns the LLM-facing string. The base content is
// the bounded skill text. When truncation occurred, a recovery
// hint is appended that names the temp file and recommends
// using a more specific skill or reading the temp file.
func (r *ReadSkillResult) MarshalLLM() string {
	if r.Truncation == nil || !r.Truncation.Truncated() {
		return r.Content
	}
	hint := truncate.RenderHint(
		ReadSkillTool.Format.RecoveryHint,
		*r.Truncation,
		map[string]string{"path": r.TempFilePath},
	)
	var sb strings.Builder
	sb.WriteString(r.Content)
	if hint != "" {
		sb.WriteString("\n\n")
		sb.WriteString(hint)
	}
	sb.WriteString(fmt.Sprintf("\n[%d lines shown of %d total; full content at %s]",
		r.Truncation.ShownLines, r.Truncation.OriginalLines, r.TempFilePath))
	return sb.String()
}

var _ artifact.LLMRenderer = (*ReadSkillResult)(nil)

// ReadSkill returns the content of a file in the named skill at the
// given skill-relative path. When path is empty, the canonical SKILL.md
// is returned; otherwise the file at skill-relative path is returned
// (e.g., path="references/foo.md").
//
// When the content exceeds the cap, the full content is written to a
// temp file and the result's TempFilePath points to it; the LLM can
// read the temp file to retrieve the rest.
//
// Parameters:
//   - name (string, required): the skill name.
//   - path (string, optional): skill-relative path. Empty (or omitted)
//     returns SKILL.md; non-empty returns the matching reference file.
func (t *Toolkit) ReadSkill(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
	name := toString(args["name"])
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	path := toString(args["path"])
	full, err := t.catalog.Read(ctx, name, path)
	if err != nil {
		return nil, err
	}

	// Apply the tool's Format cap.
	cfg := ReadSkillTool.Format.ResolvedTruncateConfig()
	style := ReadSkillTool.Format.Style
	if style == 0 {
		style = tool.StyleHead
	}
	out, trunc := truncate.Truncate(full, cfg, style)
	res := &ReadSkillResult{Content: out}
	if trunc.Truncated() {
		if tempPath, ferr := writeSkillToTemp(name, full); ferr == nil {
			res.TempFilePath = tempPath
		}
		res.Truncation = &trunc
	}
	return res, nil
}

// writeSkillToTemp writes the full skill content to a freshly
// created temp file. The temp file is created in
// os.TempDir() and is not removed automatically; the caller
// passes the path back to the LLM in the recovery hint and the
// LLM (or a follow-up call) cleans it up.
func writeSkillToTemp(name, content string) (string, error) {
	dst, err := os.CreateTemp("", "ore-skill-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer dst.Close()
	if _, err := dst.WriteString(content); err != nil {
		os.Remove(dst.Name())
		return "", fmt.Errorf("write: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(dst.Name())
		return "", fmt.Errorf("close: %w", err)
	}
	return dst.Name(), nil
}

// SystemPromptFragment returns a prompt fragment function suitable for
// systemprompt.WithContextContentFunc. The returned function delegates to
// the underlying Catalog.SystemPromptFragment, producing a formatted listing
// of all discovered skills.
func (t *Toolkit) SystemPromptFragment() func(context.Context) string {
	return t.catalog.SystemPromptFragment
}

// SetDirective overrides the default behavioral directive used in the
// system prompt fragment. It delegates to the underlying Catalog and
// is safe for concurrent use.
func (t *Toolkit) SetDirective(directive string) {
	t.catalog.SetDirective(directive)
}

// Register adds the read_skill tool to the registry.
func (t *Toolkit) Register(registry tool.Registry) error {
	if err := registry.Register(ReadSkillTool, t.ReadSkill); err != nil {
		return fmt.Errorf("register read_skill: %w", err)
	}
	return nil
}

// ReadSkillDisplayHint returns a human-readable display for read_skill calls.
func ReadSkillDisplayHint(args map[string]any) any {
	return fmt.Sprintf("📖 read_skill(%s)", toString(args["name"]))
}

// ReadSkillTool is the tool.Tool descriptor for read_skill.
var ReadSkillTool = tool.Tool{
	Name: "read_skill",
	Description: "Read a file from a named skill. Use this to load the canonical " +
		"SKILL.md or any reference file (e.g., references/foo.md). " +
		"Omit path to read SKILL.md.\n\n" +
		"Output limits: capped at 50 KB / 2000 lines, head style. " +
		"When the file exceeds the cap, the full content is " +
		"written to a temp file and only the head is returned.\n\n" +
		"Recovery: the result includes the temp file path when " +
		"truncation occurs. Use read_file on the path, or invoke a " +
		"more specific skill.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The name of the skill to read.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Skill-relative path to a reference file (e.g., references/foo.md). Omit to read SKILL.md.",
			},
		},
		"required": []string{"name"},
	},
	DisplayHint: ReadSkillDisplayHint,
	Format: tool.Format{
		Truncate: tool.TruncateConfig{
			MaxBytes: frameworkDefaultByteCap,
			MaxLines: 2000,
		},
		Style:        tool.StyleHead,
		RecoveryHint: "Output truncated. Read the full skill content at {path}, or invoke a more specific skill.",
	},
}

// toString safely extracts a string value from a JSON-decoded argument.
func toString(v any) string {
	s, _ := v.(string)
	return s
}

// Ensure filepath is used in the file when the skills tool is
// compiled (defensive against future refactors that remove
// writeSkillToTemp's only consumer; the import is here to keep
// the dependency explicit).
var _ = filepath.Separator
