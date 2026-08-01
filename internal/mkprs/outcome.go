package mkprs

import "fmt"

// outcome is the closed set of ways a repo can end up. A skip is a normal
// result (dirty tree, nothing to do), not an error, hence three states rather
// than an error and its absence.
//
// Each variant renders and counts itself, so nothing outside has to know which
// one it is holding. Each constructor requires the data its variant carries, so
// a skip without a reason cannot be built.
type outcome interface {
	// report renders this result and counts it. It is also what seals the
	// interface: unexported, so no type outside this package can implement it.
	report(r *reporter, name string)
}

type outcomeSuccess struct{ prURL string }

type outcomeSkipped struct{ reason string }

type outcomeFailed struct {
	reason string
	// c is the repo's output, replayed under the ❌ line. Nothing writes to it
	// after this outcome is built: a failure skips cleanup, so the capture is
	// complete by the time the caller reports.
	c *capture
}

func success(prURL string) outcome           { return outcomeSuccess{prURL: prURL} }
func skip(reason string) outcome             { return outcomeSkipped{reason: reason} }
func fail(reason string, c *capture) outcome { return outcomeFailed{reason: reason, c: c} }

func (o outcomeSuccess) report(r *reporter, name string) {
	// gh can exit 0 without a parseable URL on stdout. The PR still exists, so
	// this reports success and simply omits the link.
	suffix := ""
	if o.prURL != "" {
		suffix = "  " + o.prURL
	}
	fmt.Fprintf(r.out, "✅ %-*s PR created%s\n", r.width, name, suffix)
	r.succeeded++
}

func (o outcomeSkipped) report(r *reporter, name string) {
	fmt.Fprintf(r.out, "⏭️  %-*s skipped: %s\n", r.width, name, o.reason)
	r.skipped++
}

func (o outcomeFailed) report(r *reporter, name string) {
	fmt.Fprintf(r.out, "❌ %-*s %s\n", r.width, name, o.reason)
	// Under --verbose the output has already streamed past; don't repeat it.
	if !o.c.verbose && !o.c.empty() {
		fmt.Fprint(r.out, o.c.indented())
	}
	r.failed++
}
