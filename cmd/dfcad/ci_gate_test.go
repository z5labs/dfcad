// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import "testing"

// TestCIGate fails on purpose. It exists for exactly one commit, to prove that
// the required check turns red and blocks the merge, and is reverted straight
// after.
func TestCIGate(t *testing.T) {
	t.Error("deliberately failing test: proving the required check blocks a merge")
}
