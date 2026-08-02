package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type skillManifest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	Version     string   `json:"version"`
}

type evalCriteria struct {
	Context   string `json:"context"`
	Type      string `json:"type"`
	Checklist []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		MaxScore    int    `json:"max_score"`
	} `json:"checklist"`
}

func TestSkillPackageIsStandaloneAndPrivateDataFree(t *testing.T) {
	t.Parallel()

	root := skillRoot(t)
	skill := readFile(t, filepath.Join(root, "SKILL.md"))
	name, description := skillFrontmatter(t, skill)
	if name != "autoreview" {
		t.Fatalf("skill name = %q", name)
	}

	var manifest skillManifest
	decodeJSONFile(t, filepath.Join(root, ".tessl-plugin", "plugin.json"), &manifest)
	if manifest.Name != "uinaf/autoreview" || manifest.Description != description || !slices.Equal(manifest.Skills, []string{"."}) || manifest.Version != "0.1.0" {
		t.Fatalf("manifest drifted from SKILL.md: %+v", manifest)
	}

	agentMetadata := readFile(t, filepath.Join(root, "agents", "openai.yaml"))
	for _, required := range []string{
		`display_name: "Autoreview"`,
		`short_description: "Close out code changes with one independent review"`,
		`default_prompt: "Use $autoreview`,
	} {
		if !strings.Contains(agentMetadata, required) {
			t.Fatalf("agents/openai.yaml missing %q", required)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("standalone skill must not bundle helper scripts: %v", err)
	}
	allowedExtensions := map[string]bool{".md": true, ".json": true, ".yaml": true, ".txt": true}
	privatePatterns := []*regexp.Regexp{
		regexp.MustCompile(`/Users/[A-Za-z0-9._-]+`),
		regexp.MustCompile(`file://`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token)=[^[:space:]]+`),
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not standalone: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !allowedExtensions[filepath.Ext(path)] {
			return fmt.Errorf("unexpected packaged file type: %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range privatePatterns {
			if pattern.Match(content) {
				return fmt.Errorf("private-data pattern %q in %s", pattern, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	validateSkillLinks(t, root, skill)
}

func TestSkillEvalScenariosAreComplete(t *testing.T) {
	t.Parallel()

	evals := filepath.Join(skillRoot(t), "evals")
	entries, err := os.ReadDir(evals)
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "scenario-") {
			scenarios = append(scenarios, entry.Name())
		}
	}
	if len(scenarios) != 12 {
		t.Fatalf("scenario count = %d", len(scenarios))
	}
	for index := 0; index < len(scenarios); index++ {
		directory := filepath.Join(evals, fmt.Sprintf("scenario-%d", index))
		files, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("scenario-%d: %v", index, err)
		}
		var names []string
		for _, file := range files {
			if !file.IsDir() {
				names = append(names, file.Name())
			}
		}
		slices.Sort(names)
		if !slices.Equal(names, []string{"capability.txt", "criteria.json", "task.md"}) {
			t.Fatalf("scenario-%d files = %v", index, names)
		}
		if strings.TrimSpace(readFile(t, filepath.Join(directory, "capability.txt"))) == "" {
			t.Fatalf("scenario-%d has empty capability", index)
		}
		task := readFile(t, filepath.Join(directory, "task.md"))
		for _, leaked := range []string{"this eval", "this simulation", "not a real"} {
			if strings.Contains(strings.ToLower(task), leaked) {
				t.Fatalf("scenario-%d task leaks evaluation framing %q", index, leaked)
			}
		}
		var criteria evalCriteria
		decodeJSONFile(t, filepath.Join(directory, "criteria.json"), &criteria)
		if strings.TrimSpace(criteria.Context) == "" || criteria.Type != "weighted_checklist" || len(criteria.Checklist) != 10 {
			t.Fatalf("scenario-%d criteria shape = %+v", index, criteria)
		}
		total := 0
		for _, item := range criteria.Checklist {
			if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Description) == "" || item.MaxScore <= 0 {
				t.Fatalf("scenario-%d invalid checklist item = %+v", index, item)
			}
			total += item.MaxScore
		}
		if total != 100 {
			t.Fatalf("scenario-%d score = %d", index, total)
		}
	}

	var instructions struct {
		Instructions []json.RawMessage `json:"instructions"`
	}
	decodeJSONFile(t, filepath.Join(evals, "instructions.json"), &instructions)
	if len(instructions.Instructions) != 21 {
		t.Fatalf("instruction inventory count = %d", len(instructions.Instructions))
	}
	var summary struct {
		TotalScenarios int `json:"total_scenarios"`
		Coverage       struct {
			Total   int `json:"total_instructions"`
			Tested  int `json:"instructions_tested"`
			Percent int `json:"coverage_percentage"`
		} `json:"instructions_coverage"`
		ReasonDistribution map[string]int    `json:"reason_distribution"`
		Scenarios          []json.RawMessage `json:"scenarios"`
	}
	decodeJSONFile(t, filepath.Join(evals, "summary.json"), &summary)
	if summary.TotalScenarios != 12 || len(summary.Scenarios) != 12 || summary.Coverage.Total != 21 || summary.Coverage.Tested != 21 || summary.Coverage.Percent != 100 {
		t.Fatalf("eval summary is inconsistent: %+v", summary)
	}
}

func TestSkillAutoreviewCommandsUseCurrentCLIFlags(t *testing.T) {
	t.Parallel()

	root := skillRoot(t)
	paths := []string{
		filepath.Join(root, "SKILL.md"),
		filepath.Join(root, "references", "configuration.md"),
	}
	var reviewHelp bytes.Buffer
	if exit := run(t.Context(), []string{"review", "--help"}, &reviewHelp, io.Discard, dependencies{}); exit != 0 {
		t.Fatalf("review help exit = %d", exit)
	}
	var configHelp bytes.Buffer
	if exit := run(t.Context(), []string{"config", "--help"}, &configHelp, io.Discard, dependencies{}); exit != 0 {
		t.Fatalf("config help exit = %d", exit)
	}
	for _, path := range paths {
		for _, line := range strings.Split(readFile(t, path), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "autoreview ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			help := reviewHelp.String()
			if fields[1] == "config" {
				help = configHelp.String()
			}
			for _, field := range fields[2:] {
				if !strings.HasPrefix(field, "--") {
					continue
				}
				flagName := strings.TrimPrefix(strings.Trim(field, "`\"'"), "--")
				if !strings.Contains(help, "-"+flagName) {
					t.Fatalf("%s documents unknown %s flag %q", path, fields[1], flagName)
				}
			}
		}
	}
	for _, deprecated := range []string{"--panel", "--reviewers", "--prompt-file", "--dataset"} {
		if strings.Contains(readFile(t, filepath.Join(root, "SKILL.md")), deprecated) {
			t.Fatalf("standalone skill retained legacy flag %s", deprecated)
		}
	}
}

func skillRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "skills", "autoreview"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func decodeJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(readFile(t, path)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		t.Fatalf("%s contains multiple JSON values", path)
	}
}

func skillFrontmatter(t *testing.T, content string) (string, string) {
	t.Helper()
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		t.Fatal("SKILL.md frontmatter delimiters are invalid")
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(parts[1]), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("invalid frontmatter line %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name != "name" && name != "description" {
			t.Fatalf("unexpected frontmatter key %q", name)
		}
		if strings.HasPrefix(value, `"`) {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				t.Fatal(err)
			}
			value = unquoted
		}
		values[name] = value
	}
	if len(values) != 2 || values["name"] == "" || values["description"] == "" {
		t.Fatalf("frontmatter values = %+v", values)
	}
	return values["name"], values["description"]
}

func validateSkillLinks(t *testing.T, root, content string) {
	t.Helper()
	links := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`).FindAllStringSubmatch(content, -1)
	for _, match := range links {
		target := match[1]
		if filepath.IsAbs(target) || strings.Contains(target, "://") {
			t.Fatalf("skill link is not standalone-relative: %q", target)
		}
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(target)))
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("skill link escapes package: %q", target)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("skill link %q: %v", target, err)
		}
	}
}
