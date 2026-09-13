// Package vault implements the in-memory hub index: every issue, doc, and
// memory under a hub directory, their outbound and inbound wikilinks, and the
// queries and filesystem watcher built on top of it.
package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/markdown"
	"github.com/mattsp1290/beans/plan"
)

// Kind is the kind of note indexed from the hub.
type Kind string

const (
	KindIssue   Kind = "issue"
	KindDoc     Kind = "doc"
	KindMemory  Kind = "memory"
	KindRequest Kind = "request"
	KindPlan    Kind = "plan"
	KindHandoff Kind = "handoff"
)

// LinkKind is the origin of one outbound link from a note.
type LinkKind string

const (
	LinkBody         LinkKind = "body"
	LinkParent       LinkKind = "parent"
	LinkBlockedBy    LinkKind = "blocked_by"
	LinkRequestIssue LinkKind = "request_issue"
	LinkEmbed        LinkKind = "embed"
	LinkHandoffIssue LinkKind = "handoff_issue"
)

// LinkRef is one link between two notes, or from a note to an unresolved
// target. From and To are note basenames; To is the raw target text when the
// link did not resolve.
type LinkRef struct {
	From string
	To   string
	Kind LinkKind
}

// rawLink is a link target as parsed from a file, before resolution.
type rawLink struct {
	to   string
	kind LinkKind
}

// Note is one indexed markdown file: an issue, a doc, or a memory.
type Note struct {
	Kind        Kind
	Path        string // hub-relative, slash-separated
	Basename    string // filename without .md
	Project     string
	Title       string
	Tags        []string
	Outlinks    []LinkRef
	Frontmatter map[string]any
	Issue       *issue.Issue   // set for KindIssue
	Memory      *issue.Memory  // set for KindMemory
	Request     *issue.Request // set for KindRequest
	Plan        *plan.Plan     // set for KindPlan
	Handoff     *issue.Handoff // set for KindHandoff

	rawOut  []rawLink // link targets before resolution
	docBody string    // doc body, kept for Search; issues/memories keep it on Issue/Memory
}

// Project is one projects/<name> directory in the hub.
type Project struct {
	Name     string
	Dir      string
	Config   issue.ProjectConfig
	Workflow issue.WorkflowConfig
}

// Warning is a non-fatal problem found while loading or reloading the index.
type Warning struct {
	Path string
	Err  error
}

// Index is the in-memory hub index.
//
// Concurrency: Index embeds a sync.RWMutex. Load builds a new Index without
// locking. Reload takes the write lock. Every other method and every direct
// read of the exported maps must be done while holding RLock, so a server
// that serves reads while a Watch goroutine reloads must wrap each request
// in ix.RLock()/ix.RUnlock().
type Index struct {
	sync.RWMutex

	HubDir    string
	HubConfig issue.HubConfig
	Workflow  issue.WorkflowConfig // hub-level
	Projects  map[string]*Project
	Issues    map[string]*issue.Issue   // by id
	Requests  map[string]*issue.Request // by id
	Plans     map[string]*plan.Plan     // by stable id
	Handoffs  map[string]*issue.Handoff // by id
	Notes     map[string]*Note          // by basename; on a collision the first in walk order wins
	ByPath    map[string]*Note          // by hub-relative path; every note, collisions included
	Aliases   map[string]string         // alias -> basename
	Backlinks map[string][]LinkRef      // basename -> refs pointing at it
	Assets    map[string]bool           // hub-relative paths of image files
	Warnings  []Warning

	// ExplicitWorkflow is the BN_CONFIG path used to load workflow config, if any.
	ExplicitWorkflow string

	// order holds every currently-indexed note in the order it was
	// (re)loaded, which for Load is sorted walk order. It drives
	// deterministic alias/backlink rebuilding and search ranking ties.
	order []*Note

	// parseWarnings holds parse failures and duplicate-basename warnings,
	// keyed by the offending file's hub-relative path, so Reload can
	// replace or clear them without losing warnings for untouched files.
	parseWarnings map[string]error
	// linkWarnings holds "unresolved link" warnings, recomputed in full
	// every time resolveOutlinks runs.
	linkWarnings []Warning
	// dupWarnings holds duplicate-basename and duplicate-id warnings,
	// recomputed by rebuild.
	dupWarnings []Warning

	hubTOML []byte
}

// LoadOptions configures LoadWithOptions.
type LoadOptions struct {
	// ExplicitWorkflow is the BN_CONFIG path (see issue.LoadWorkflow).
	ExplicitWorkflow string
}

// Load builds the index for the hub at hubDir. It is LoadWithOptions with the
// zero LoadOptions.
func Load(hubDir string) (*Index, error) {
	return LoadWithOptions(hubDir, LoadOptions{})
}

// imageExts are the extensions treated as assets under docs/, issues/, and
// archive/ directories.
var imageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".svg":  true,
	".webp": true,
}

// LoadWithOptions builds the index for the hub at hubDir.
func LoadWithOptions(hubDir string, opts LoadOptions) (*Index, error) {
	abs, err := filepath.Abs(hubDir)
	if err != nil {
		return nil, fmt.Errorf("vault: resolve hub dir %s: %w", hubDir, err)
	}

	ix := &Index{
		HubDir:           abs,
		Projects:         map[string]*Project{},
		Issues:           map[string]*issue.Issue{},
		Requests:         map[string]*issue.Request{},
		Plans:            map[string]*plan.Plan{},
		Handoffs:         map[string]*issue.Handoff{},
		Notes:            map[string]*Note{},
		ByPath:           map[string]*Note{},
		Aliases:          map[string]string{},
		Backlinks:        map[string][]LinkRef{},
		Assets:           map[string]bool{},
		ExplicitWorkflow: opts.ExplicitWorkflow,
		parseWarnings:    map[string]error{},
	}

	hubTOMLPath := filepath.Join(abs, "beans.toml")
	hubCfg, err := issue.LoadHubConfig(hubTOMLPath)
	if err != nil {
		return nil, fmt.Errorf("vault: load hub config: %w", err)
	}
	ix.HubConfig = hubCfg
	hubTOML, err := os.ReadFile(hubTOMLPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("vault: read %s: %w", hubTOMLPath, err)
	}
	ix.hubTOML = hubTOML

	hubWF, err := issue.LoadWorkflow(opts.ExplicitWorkflow, nil, hubTOML)
	if err != nil {
		return nil, fmt.Errorf("vault: hub workflow: %w", err)
	}
	ix.Workflow = hubWF

	projectTOMLs, err := filepath.Glob(filepath.Join(abs, "projects", "*", "beans.toml"))
	if err != nil {
		return nil, fmt.Errorf("vault: glob projects: %w", err)
	}
	sort.Strings(projectTOMLs)
	for _, tomlPath := range projectTOMLs {
		dir := filepath.Dir(tomlPath)
		name := filepath.Base(dir)
		cfg, err := issue.LoadProjectConfig(tomlPath)
		if err != nil {
			return nil, fmt.Errorf("vault: load project config %s: %w", tomlPath, err)
		}
		raw, err := os.ReadFile(tomlPath)
		if err != nil {
			return nil, fmt.Errorf("vault: read %s: %w", tomlPath, err)
		}
		wf, err := issue.LoadWorkflow(opts.ExplicitWorkflow, raw, hubTOML)
		if err != nil {
			return nil, fmt.Errorf("vault: workflow for project %s: %w", name, err)
		}
		ix.Projects[name] = &Project{Name: name, Dir: dir, Config: cfg, Workflow: wf}
	}

	if err := ix.walkAndIndex(abs); err != nil {
		return nil, err
	}
	ix.rebuild()
	return ix, nil
}

// walkAndIndex walks root, indexing markdown notes and recording assets, in
// sorted directory order.
func (ix *Index) walkAndIndex(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() {
			if isPlansDirectory(rel) {
				if err := gitops.RecoverTrees(path); err != nil {
					return fmt.Errorf("vault: recover plan trees in %s: %w", rel, err)
				}
			}
			if skipDirName(name) {
				return filepath.SkipDir
			}
			if project, ok := planBundlePath(rel); ok {
				ix.loadPlan(root, project, rel)
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if isPlanRootEntry(rel) {
			ix.addParseWarning(rel, fmt.Errorf("plan root must be a directory"))
			return nil
		}
		if project, ok := planManifestPath(rel); ok {
			ix.loadPlan(root, project, filepath.ToSlash(filepath.Dir(rel)))
		} else {
			ix.loadPath(root, rel)
		}
		return nil
	})
}

func isPlansDirectory(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 3 && parts[0] == "projects" && parts[1] != "" && parts[2] == "plans"
}

func planBundlePath(rel string) (string, bool) {
	s := strings.Split(rel, "/")
	if len(s) == 4 && s[0] == "projects" && s[2] == "plans" {
		return s[1], true
	}
	return "", false
}
func isPlanRootEntry(rel string) bool {
	s := strings.Split(rel, "/")
	return len(s) == 4 && s[0] == "projects" && s[2] == "plans"
}

func planManifestPath(rel string) (string, bool) {
	s := strings.Split(rel, "/")
	return func() (string, bool) {
		if len(s) == 5 && s[0] == "projects" && s[2] == "plans" && s[4] == "plan.md" {
			return s[1], true
		}
		return "", false
	}()
}

func (ix *Index) loadPlan(root, project, rel string) {
	if err := gitops.RecoverPlanTemp(filepath.Join(root, filepath.FromSlash(rel), "plan.md")); err != nil {
		ix.addParseWarning(rel, err)
		return
	}
	b, err := plan.Load(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		ix.addParseWarning(rel, err)
		return
	}
	projectRecord, exists := ix.Projects[project]
	if !exists {
		ix.addParseWarning(rel, fmt.Errorf("plan project %q has no beans.toml", project))
		return
	}
	if !plan.ValidID(projectRecord.Config.Prefix, b.Plan.ID) && !plan.ValidID(project, b.Plan.ID) {
		ix.addParseWarning(rel, fmt.Errorf("plan id %q does not match project", b.Plan.ID))
		return
	}
	b.Plan.Path = rel + "/plan.md"
	n := &Note{Kind: KindPlan, Path: b.Plan.Path, Basename: b.Plan.ID, Project: project, Title: b.Plan.Title, Plan: b.Plan}
	n.rawOut = linksToRaw(markdown.Links([]byte(b.Plan.Body)))
	for _, s := range b.Sections {
		n.rawOut = append(n.rawOut, linksToRaw(markdown.Links([]byte(s.Markdown)))...)
	}
	ix.registerNote(b.Plan.ID, n)
}

func skipDirName(name string) bool {
	return strings.HasPrefix(name, ".") || name == "templates"
}

// loadPath reads and indexes the single file at root/rel (rel is
// hub-relative, slash-separated), or records it as an asset.
func (ix *Index) loadPath(root, rel string) {
	if isAssetPath(rel) {
		ix.Assets[rel] = true
		return
	}
	kind, project, ok := classify(rel)
	if !ok {
		return
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		ix.addParseWarning(rel, err)
		return
	}
	ix.indexFile(kind, project, rel, data)
}

// classify reports whether relPath (hub-relative, slash-separated) is an
// indexable markdown note, its Kind, and its owning project ("" for
// hub-level notes).
func classify(relPath string) (kind Kind, project string, ok bool) {
	if !strings.HasSuffix(relPath, ".md") {
		return "", "", false
	}
	segs := strings.Split(relPath, "/")
	if segs[0] == "projects" {
		if len(segs) < 4 {
			return "", "", false
		}
		project = segs[1]
		rest := segs[2:]
		switch rest[0] {
		case "issues":
			if len(rest) == 2 {
				return KindIssue, project, true
			}
		case "archive":
			if len(rest) >= 2 {
				return KindIssue, project, true
			}
		case "docs":
			if len(rest) >= 2 {
				return KindDoc, project, true
			}
		case "memories":
			if len(rest) == 2 {
				return KindMemory, project, true
			}
		case "requests":
			if len(rest) == 2 {
				return KindRequest, project, true
			}
		case "handoffs":
			if len(rest) == 2 || (len(rest) == 4 && rest[1] == "archive" && len(rest[2]) == 4) {
				return KindHandoff, project, true
			}
		}
		return "", "", false
	}
	switch segs[0] {
	case "docs":
		if len(segs) >= 2 {
			return KindDoc, "", true
		}
	case "memories":
		if len(segs) == 2 {
			return KindMemory, "", true
		}
	}
	return "", "", false
}

// isAssetPath reports whether relPath is an image file under a docs/,
// issues/, or archive/ directory at hub or project level.
func isAssetPath(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	if !imageExts[ext] {
		return false
	}
	segs := strings.Split(relPath, "/")
	if segs[0] == "projects" {
		if len(segs) < 4 {
			return false
		}
		switch segs[2] {
		case "docs", "issues", "archive":
			return true
		}
		return false
	}
	if len(segs) < 2 {
		return false
	}
	switch segs[0] {
	case "docs", "issues", "archive":
		return true
	}
	return false
}

// indexFile parses data (the contents of the file at rel) per kind and, on
// success, registers the resulting note.
func (ix *Index) indexFile(kind Kind, project, rel string, data []byte) {
	basename := strings.TrimSuffix(pathBase(rel), ".md")

	switch kind {
	case KindIssue:
		iss, err := issue.Parse(rel, data)
		if err != nil {
			ix.addParseWarning(rel, err)
			return
		}
		note := &Note{
			Kind:     KindIssue,
			Path:     rel,
			Basename: basename,
			Project:  iss.Project,
			Title:    iss.Title,
			Tags:     append([]string(nil), iss.Labels...),
			Issue:    iss,
		}
		var raw []rawLink
		raw = append(raw, linksToRaw(markdown.Links([]byte(iss.Description+iss.Body)))...)
		if !iss.Parent.IsZero() {
			raw = append(raw, rawLink{to: iss.Parent.Target, kind: LinkParent})
		}
		for _, b := range iss.BlockedBy {
			if b.IsZero() {
				continue
			}
			raw = append(raw, rawLink{to: b.Target, kind: LinkBlockedBy})
		}
		note.rawOut = raw
		ix.registerNote(basename, note)

	case KindMemory:
		mem, err := issue.ParseMemory(rel, data)
		if err != nil {
			ix.addParseWarning(rel, err)
			return
		}
		note := &Note{
			Kind:     KindMemory,
			Path:     rel,
			Basename: basename,
			Project:  mem.Project,
			Title:    mem.Key,
			Tags:     append([]string(nil), mem.Tags...),
			Memory:   mem,
		}
		note.rawOut = linksToRaw(markdown.Links([]byte(mem.Body)))
		ix.registerNote(basename, note)

	case KindRequest:
		req, err := issue.ParseRequest(rel, data)
		if err != nil {
			ix.addParseWarning(rel, err)
			return
		}
		note := &Note{Kind: KindRequest, Path: rel, Basename: basename, Project: req.Project, Title: req.Title, Tags: append([]string(nil), req.Labels...), Request: req}
		raw := linksToRaw(markdown.Links([]byte(req.Body)))
		for _, link := range req.Issues {
			if !link.IsZero() {
				raw = append(raw, rawLink{to: link.Target, kind: LinkRequestIssue})
			}
		}
		note.rawOut = raw
		ix.registerNote(basename, note)

	case KindHandoff:
		h, err := issue.ParseHandoff(rel, data)
		if err != nil {
			ix.addParseWarning(rel, err)
			return
		}
		note := &Note{Kind: KindHandoff, Path: rel, Basename: basename, Project: h.Project, Title: h.Title, Handoff: h,
			Frontmatter: map[string]any{"id": h.ID, "aliases": h.Aliases, "title": h.Title, "issue": h.Issue.Raw, "created": h.Created, "updated": h.Updated}}
		note.rawOut = linksToRaw(markdown.Links([]byte(h.Body)))
		if !h.Issue.IsZero() {
			note.rawOut = append(note.rawOut, rawLink{to: h.Issue.Target, kind: LinkHandoffIssue})
		}
		ix.registerNote(basename, note)

	case KindDoc:
		fmBytes, body := splitDocFrontmatter(data)
		var fmMap map[string]any
		if len(strings.TrimSpace(string(fmBytes))) > 0 {
			_ = yaml.Unmarshal(fmBytes, &fmMap)
		}
		title := stringField(fmMap, "title")
		if title == "" {
			title = firstHeading(body)
		}
		if title == "" {
			title = basename
		}
		note := &Note{
			Kind:        KindDoc,
			Path:        rel,
			Basename:    basename,
			Project:     project,
			Title:       title,
			Tags:        stringListField(fmMap, "tags"),
			Frontmatter: fmMap,
			docBody:     string(body),
		}
		note.rawOut = linksToRaw(markdown.Links(body))
		ix.registerNote(basename, note)
	}
}

// linksToRaw converts markdown.Links output into rawLinks, choosing LinkEmbed
// for the ![[ ]] form.
func linksToRaw(links []markdown.Link) []rawLink {
	out := make([]rawLink, 0, len(links))
	for _, l := range links {
		k := LinkBody
		if l.Embed {
			k = LinkEmbed
		}
		out = append(out, rawLink{to: l.Target, kind: k})
	}
	return out
}

// registerNote records note in walk order and by path. Notes and Issues
// are derived from that order by rebuild, so a basename collision keeps
// every file indexed (the first in walk order owns the basename).
func (ix *Index) registerNote(basename string, note *Note) {
	note.Basename = basename
	ix.order = append(ix.order, note)
	ix.ByPath[note.Path] = note
}

func (ix *Index) addParseWarning(path string, err error) {
	ix.parseWarnings[path] = err
}

// rebuild recomputes Aliases and Backlinks (and per-note Outlinks) from the
// current set of notes, then recomputes Warnings. It must be called after
// every batch of note additions/removals (Load, Reload).
func (ix *Index) rebuild() {
	notes := map[string]*Note{}
	issues := map[string]*issue.Issue{}
	requests := map[string]*issue.Request{}
	plans := map[string]*plan.Plan{}
	handoffs := map[string]*issue.Handoff{}
	var dups []Warning
	for _, n := range ix.order {
		if first, dup := notes[n.Basename]; dup {
			dups = append(dups, Warning{Path: n.Path, Err: fmt.Errorf("duplicate note basename %q (also %s); links resolve to the first", n.Basename, first.Path)})
		} else {
			notes[n.Basename] = n
		}
		if n.Kind == KindIssue && n.Issue != nil {
			if first, dup := issues[n.Issue.ID]; dup {
				dups = append(dups, Warning{Path: n.Path, Err: fmt.Errorf("duplicate issue id %q (also %s); the first is used", n.Issue.ID, first.Path)})
			} else {
				issues[n.Issue.ID] = n.Issue
			}
		}
		if n.Kind == KindRequest && n.Request != nil {
			if first, dup := requests[n.Request.ID]; dup {
				dups = append(dups, Warning{Path: n.Path, Err: fmt.Errorf("duplicate request id %q (also %s); the first is used", n.Request.ID, first.Path)})
			} else {
				requests[n.Request.ID] = n.Request
			}
		}
		if n.Kind == KindPlan && n.Plan != nil {
			if first, dup := plans[n.Plan.ID]; dup {
				dups = append(dups, Warning{Path: n.Path, Err: fmt.Errorf("duplicate plan id %q (also %s); the first is used", n.Plan.ID, first.Path)})
			} else {
				plans[n.Plan.ID] = n.Plan
			}
		}
		if n.Kind == KindHandoff && n.Handoff != nil {
			if first, dup := handoffs[n.Handoff.ID]; dup {
				dups = append(dups, Warning{Path: n.Path, Err: fmt.Errorf("duplicate handoff id %q (also %s); the first is used", n.Handoff.ID, first.Path)})
			} else {
				handoffs[n.Handoff.ID] = n.Handoff
			}
		}
	}
	ix.Notes = notes
	ix.Issues = issues
	ix.Requests = requests
	ix.Plans = plans
	ix.Handoffs = handoffs
	ix.dupWarnings = dups
	aliases := map[string]string{}
	for _, n := range ix.order {
		for _, a := range noteAliases(n) {
			a = strings.TrimSpace(a)
			if a == "" || a == n.Basename {
				continue
			}
			if _, exists := aliases[a]; !exists {
				aliases[a] = n.Basename
			}
		}
	}
	ix.Aliases = aliases
	ix.resolveOutlinks()
	ix.finalizeWarnings()
}

func noteAliases(n *Note) []string {
	switch n.Kind {
	case KindIssue:
		if n.Issue != nil {
			return n.Issue.Aliases
		}
	case KindDoc:
		return stringListField(n.Frontmatter, "aliases")
	case KindRequest:
		if n.Request != nil {
			return n.Request.Aliases
		}
	case KindPlan:
		if n.Plan != nil {
			return n.Plan.Aliases
		}
	}
	if n.Handoff != nil {
		return n.Handoff.Aliases
	}
	return nil
}

// resolveOutlinks resolves every note's raw links against the current index,
// populating Outlinks, Backlinks, and linkWarnings.
func (ix *Index) resolveOutlinks() {
	backlinks := map[string][]LinkRef{}
	var warnings []Warning
	for _, n := range ix.order {
		out := make([]LinkRef, 0, len(n.rawOut))
		for _, r := range n.rawOut {
			ref := LinkRef{From: n.Basename, Kind: r.kind}
			if target, ok := ix.Lookup(r.to); ok {
				ref.To = target.Basename
				backlinks[target.Basename] = append(backlinks[target.Basename], ref)
			} else {
				ref.To = r.to
				warnings = append(warnings, Warning{Path: n.Path, Err: fmt.Errorf("unresolved link [[%s]] in %s", r.to, n.Path)})
			}
			out = append(out, ref)
		}
		n.Outlinks = out
	}
	ix.Backlinks = backlinks
	ix.linkWarnings = warnings
}

func (ix *Index) finalizeWarnings() {
	paths := make([]string, 0, len(ix.parseWarnings))
	for p := range ix.parseWarnings {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]Warning, 0, len(paths)+len(ix.linkWarnings))
	for _, p := range paths {
		out = append(out, Warning{Path: p, Err: ix.parseWarnings[p]})
	}
	out = append(out, ix.dupWarnings...)
	out = append(out, ix.linkWarnings...)
	ix.Warnings = out
}

// Lookup resolves target against the index. A target with a path is matched
// against note paths first (exact hub-relative path, then a unique path
// suffix); then the last segment is resolved as a basename: exact basename,
// alias, issue id, case-insensitive basename. A trailing .md is ignored.
func (ix *Index) Lookup(target string) (*Note, bool) {
	t := strings.Trim(strings.TrimSpace(target), "/")
	if strings.Contains(t, "/") {
		withMD := t
		if !strings.HasSuffix(withMD, ".md") {
			withMD += ".md"
		}
		if n, ok := ix.ByPath[withMD]; ok {
			return n, true
		}
		var match *Note
		for path, n := range ix.ByPath {
			if strings.HasSuffix(path, "/"+withMD) {
				if match != nil {
					match = nil
					break
				}
				match = n
			}
		}
		if match != nil {
			return match, true
		}
		t = t[strings.LastIndexByte(t, '/')+1:]
	}
	t = strings.TrimSuffix(t, ".md")

	if n, ok := ix.Notes[t]; ok {
		return n, true
	}
	if basename, ok := ix.Aliases[t]; ok {
		if n, ok := ix.Notes[basename]; ok {
			return n, true
		}
	}
	if iss, ok := ix.Issues[t]; ok {
		basename := strings.TrimSuffix(pathBase(iss.Path), ".md")
		if n, ok := ix.Notes[basename]; ok {
			return n, true
		}
	}
	if h, ok := ix.Handoffs[t]; ok {
		if n, ok := ix.ByPath[h.Path]; ok {
			return n, true
		}
	}
	lower := strings.ToLower(t)
	for basename, n := range ix.Notes {
		if strings.ToLower(basename) == lower {
			return n, true
		}
	}
	return nil, false
}

// Reload re-parses only the given files (absolute or hub-relative) and
// rebuilds the derived maps. Removed files drop out of the index; a basename
// freed by a removal is taken over by the next note in walk order. State for
// files not passed is left untouched. A beans.toml path (hub or project)
// reloads the whole index, because workflow vocabularies affect every query.
// Reload takes the write lock.
func (ix *Index) Reload(paths ...string) error {
	ix.Lock()
	defer ix.Unlock()
	rels := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := ix.toRelPath(p)
		if err != nil {
			return err
		}
		if pathBase(rel) == "beans.toml" {
			return ix.reloadAll()
		}
		rels = append(rels, rel)
	}
	for _, rel := range rels {
		ix.reloadOne(rel)
	}
	ix.rebuild()
	return nil
}

// ReloadAll rebuilds the whole index from disk under the write lock; bn serve
// calls it after every mutation so the next request sees the commit.
func (ix *Index) ReloadAll() error {
	ix.Lock()
	defer ix.Unlock()
	return ix.reloadAll()
}

// reloadAll rebuilds the index from disk in place (caller holds the lock).
func (ix *Index) reloadAll() error {
	fresh, err := LoadWithOptions(ix.HubDir, LoadOptions{ExplicitWorkflow: ix.ExplicitWorkflow})
	if err != nil {
		return err
	}
	ix.HubConfig, ix.Workflow, ix.Projects = fresh.HubConfig, fresh.Workflow, fresh.Projects
	ix.Issues, ix.Requests, ix.Plans, ix.Handoffs, ix.Notes, ix.ByPath, ix.Aliases, ix.Backlinks = fresh.Issues, fresh.Requests, fresh.Plans, fresh.Handoffs, fresh.Notes, fresh.ByPath, fresh.Aliases, fresh.Backlinks
	ix.Assets, ix.Warnings = fresh.Assets, fresh.Warnings
	ix.order, ix.parseWarnings, ix.linkWarnings, ix.dupWarnings, ix.hubTOML = fresh.order, fresh.parseWarnings, fresh.linkWarnings, fresh.dupWarnings, fresh.hubTOML
	return nil
}

func (ix *Index) toRelPath(p string) (string, error) {
	rel := p
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(ix.HubDir, p)
		if err != nil {
			return "", fmt.Errorf("vault: %s is not under hub %s: %w", p, ix.HubDir, err)
		}
		rel = r
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("vault: %s is not under hub %s", p, ix.HubDir)
	}
	return rel, nil
}

func (ix *Index) reloadOne(rel string) {
	if project, root, ok := planRoot(rel); ok {
		ix.reloadPlan(project, root)
		return
	}
	ix.removeNoteByPath(rel)
	delete(ix.parseWarnings, rel)
	delete(ix.Assets, rel)

	abs := filepath.Join(ix.HubDir, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return
	}
	if isAssetPath(rel) {
		ix.Assets[rel] = true
		return
	}
	kind, project, ok := classify(rel)
	if !ok {
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		ix.addParseWarning(rel, err)
		return
	}
	ix.indexFile(kind, project, rel, data)
}

// planRoot maps a manifest or section event to its aggregate bundle root.
func planRoot(rel string) (project, root string, ok bool) {
	parts := strings.Split(rel, "/")
	if len(parts) >= 5 && parts[0] == "projects" && parts[2] == "plans" {
		return parts[1], strings.Join(parts[:4], "/"), true
	}
	return "", "", false
}

// reloadPlan preserves a last known valid aggregate while an editor is in
// the middle of a multi-file update. A missing root is a real deletion.
func (ix *Index) reloadPlan(project, root string) {
	manifest := root + "/plan.md"
	abs := filepath.Join(ix.HubDir, filepath.FromSlash(root))
	if err := gitops.RecoverTree(abs); err != nil {
		ix.addParseWarning(manifest, fmt.Errorf("recover plan tree: %w", err))
		return
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		ix.removeNoteByPath(manifest)
		delete(ix.parseWarnings, manifest)
		return
	}
	b, err := plan.Load(abs)
	if err != nil {
		ix.addParseWarning(manifest, err)
		return
	}
	ix.removeNoteByPath(manifest)
	delete(ix.parseWarnings, manifest)
	// loadPlan performs the same identity and link extraction as initial load.
	_ = b
	ix.loadPlan(ix.HubDir, project, root)
}

// removeNoteByPath drops the note (if any) previously indexed from rel.
// Notes and Issues are re-derived by rebuild.
func (ix *Index) removeNoteByPath(rel string) {
	delete(ix.ByPath, rel)
	for i, n := range ix.order {
		if n.Path == rel {
			ix.order = append(ix.order[:i], ix.order[i+1:]...)
			return
		}
	}
}

// WorkflowFor returns the workflow config for project, or the hub-level
// workflow when project is unknown or "".
func (ix *Index) WorkflowFor(project string) issue.WorkflowConfig {
	if project != "" {
		if p, ok := ix.Projects[project]; ok {
			return p.Workflow
		}
	}
	return ix.Workflow
}

// IssueByID looks up an issue by id.
func (ix *Index) IssueByID(id string) (*issue.Issue, bool) {
	iss, ok := ix.Issues[id]
	return iss, ok
}

// RequestByID looks up a request by stable id.
func (ix *Index) RequestByID(id string) (*issue.Request, bool) {
	req, ok := ix.Requests[id]
	return req, ok
}

// ProjectRequests returns requests filtered and ordered for CLI and API reads.
func (ix *Index) ProjectRequests(project, status, label string, priority *int, query string, terminal bool) []*issue.Request {
	var out []*issue.Request
	q := strings.ToLower(strings.TrimSpace(query))
	for _, req := range ix.Requests {
		if project != "" && req.Project != project || status != "" && req.Status != status || label != "" && !hasString(req.Labels, label) || priority != nil && req.Priority != *priority {
			continue
		}
		if !terminal && status == "" && (req.Status == issue.RequestResolved || req.Status == issue.RequestDeclined) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(req.Title+"\n"+req.ID+"\n"+req.RequestedBy+"\n"+strings.Join(req.Labels, "\n")+"\n"+req.Body), q) {
			continue
		}
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ProjectIssues returns the issues in project ("" = all projects), sorted by
// id.
func (ix *Index) ProjectIssues(project string, includeArchived bool) []*issue.Issue {
	out := make([]*issue.Issue, 0, len(ix.Issues))
	for _, iss := range ix.Issues {
		if project != "" && iss.Project != project {
			continue
		}
		if iss.Archived && !includeArchived {
			continue
		}
		out = append(out, iss)
	}
	sortByID(out)
	return out
}

func sortByID(list []*issue.Issue) {
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
}

func hasString(list []string, want string) bool {
	for _, value := range list {
		if value == want {
			return true
		}
	}
	return false
}

// --- small helpers -----------------------------------------------------

func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func stringListField(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstHeading(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

// splitDocFrontmatter separates an optional leading YAML frontmatter block
// (delimited by "---" lines) from the rest of a doc. When there is no
// frontmatter, or the closing fence is missing, fm is nil and body is data
// unchanged.
func splitDocFrontmatter(data []byte) (fm []byte, body []byte) {
	s := string(data)
	const openFence = "---\n"
	if !strings.HasPrefix(s, openFence) {
		return nil, data
	}
	rest := s[len(openFence):]
	lines := strings.SplitAfter(rest, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if trimmed == "---" {
			fmText := strings.Join(lines[:i], "")
			bodyText := strings.Join(lines[i+1:], "")
			return []byte(fmText), []byte(bodyText)
		}
	}
	return nil, data
}
