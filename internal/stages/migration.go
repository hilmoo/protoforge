package stages

import (
	"context"
	"errors"

	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/pgsql"
	"github.com/hilmoo/protoforge/internal/runner"
	"github.com/hilmoo/protoforge/internal/syncmig"
	"github.com/hilmoo/protoforge/internal/textops"
)

// Migration wraps drizzle-kit / drizzle-docs and runs post_push_sql and sync.
type Migration struct {
	ctx *Ctx
	c   *config.Migration
}

func (m *Migration) Name() string { return "migration" }

func (m *Migration) dir() string { return m.ctx.Cfg.Abs("", m.c.Dir) }

// Steps follow the original task order: generate, docs, push (+ post-push SQL), sync.
func (m *Migration) Steps() []Step {
	return []Step{
		{Name: "generate", Enabled: m.c.Generate, Note: "migration.generate is false", Run: func() error { return m.kit("generate") }},
		{Name: "docs", Enabled: m.c.Docs != nil, Note: "migration.docs not set", Run: m.docs},
		{Name: "push", Enabled: m.c.Push, Note: "migration.push is false", Run: func() error { return m.kit("push") }},
		{Name: "post_push_sql", Enabled: m.c.Push && len(m.c.PostPushSQL) > 0, Note: "runs only after push", Run: m.postPush},
		{Name: "migrate", Enabled: m.c.Migrate, Note: "migration.migrate is false", Run: func() error { return m.kit("migrate") }},
		{Name: "sync", Enabled: m.c.Sync != nil, Note: "migration.sync not set", Run: m.sync},
		{Name: "run_after", Enabled: len(m.c.RunAfter) > 0, Run: func() error { return m.ctx.runCommands(m.Name(), m.dir(), m.c.RunAfter) }},
	}
}

func (m *Migration) kit(sub string) error {
	argv, err := m.ctx.tool(runner.ToolSpec{Name: "drizzle-kit", Bin: "drizzle-kit", Override: m.ctx.Cfg.Tools.DrizzleKit, Node: true}, m.dir())
	if err != nil {
		return err
	}
	return m.ctx.run(m.Name(), m.dir(), append(argv, sub, "--config", m.c.DrizzleConfig))
}

func (m *Migration) docs() error {
	d := m.c.Docs
	argv, err := m.ctx.tool(runner.ToolSpec{Name: "drizzle-docs", Bin: "drizzle-docs", Override: m.ctx.Cfg.Tools.DrizzleDocs, Node: true}, m.dir())
	if err != nil {
		return err
	}
	return m.ctx.run(m.Name(), m.dir(), append(argv, "generate", d.SchemaDir, "-d", d.Dialect, "-f", d.Format, "-o", d.Out, "--force"))
}

func (m *Migration) postPush() error {
	if len(m.c.PostPushSQL) == 0 {
		return errors.New("migration.post_push_sql is empty")
	}
	if !m.ctx.R.FileOp(m.Name(), "execute %d post_push_sql statement(s) in one transaction via DATABASE_URL", len(m.c.PostPushSQL)) {
		return nil
	}
	url := runner.Getenv(m.ctx.R.Env, "DATABASE_URL")
	if err := pgsql.Run(context.Background(), url, m.c.PostPushSQL); err != nil {
		return err
	}
	m.ctx.R.Logf(m.Name(), "post_push_sql committed")
	return nil
}

func (m *Migration) sync() error {
	s := m.c.Sync
	rules, err := textops.Compile(s.ExtraReplace)
	if err != nil {
		return err
	}
	to := m.ctx.Cfg.Abs(m.c.Dir, s.To)
	written, err := syncmig.Run(syncmig.Options{
		From:   m.ctx.Cfg.Abs(m.c.Dir, m.c.DrizzleOut),
		To:     to,
		Suffix: s.Suffix,
		Root:   m.ctx.Cfg.Root,
		Extra:  rules,
		DryRun: m.ctx.R.DryRun,
		Warn:   func(f string, a ...any) { m.ctx.R.Warnf(m.Name(), f, a...) },
		Op: func(verb, path string) {
			if m.ctx.R.DryRun {
				m.ctx.R.FileOp(m.Name(), "%s %s", verb, m.ctx.Cfg.Rel(path))
			}
		},
	})
	if err != nil {
		return err
	}
	if !m.ctx.R.DryRun {
		m.ctx.R.Logf(m.Name(), "Migrations synced to %s (%d files)", m.ctx.Cfg.Rel(to), len(written))
	}
	return nil
}
