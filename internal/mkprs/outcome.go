package mkprs

import "fmt"

// outcome is the closed set of ways a repo can end up. A skip is a normal
// result (nothing to do here), not an error, hence three states rather than an
// error and its absence. Each variant renders and counts itself, so nothing
// outside has to know which one it is holding.
type outcome interface {
	// report renders this result and counts it. Unexported, so no type outside
	// this package can implement outcome.
	report(r *reporter, name string)
	// failed reports whether this is the outcome that -s stops for and that
	// cleanup leaves alone -- see design-decisions.md, a failure is a repo you will
	// have to run again for. It exists so those two callers do not have to
	// assert on the concrete type and reopen what this interface closes.
	failed() bool
}

type outcomeSuccess struct{ prURL string }

type outcomeSkipped struct{ reason string }

type outcomeFailed struct {
	reason string
	// output is still being written to when this is built: the deferred
	// restoreRepo runs before the caller reports.
	output *capture
}

func success(prURL string) outcome { return outcomeSuccess{prURL: prURL} }
func skip(reason string) outcome   { return outcomeSkipped{reason: reason} }
func failure(reason string, output *capture) outcome {
	return outcomeFailed{reason: reason, output: output}
}

func (o outcomeSuccess) failed() bool { return false }
func (o outcomeSkipped) failed() bool { return false }
func (o outcomeFailed) failed() bool  { return true }

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
	if !o.output.verbose && !o.output.empty() {
		fmt.Fprint(r.out, o.output.indented())
	}
	r.failed++
}
