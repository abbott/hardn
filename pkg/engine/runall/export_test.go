package runall

import "context"

// Combine is the step that plans r's Planned rows together (combine), for
// the tests: decision 5 (a) leaves Read no conflict to show it refused.
func Combine(ctx context.Context, in Input, r *Reading) error { return r.combine(ctx, in) }
