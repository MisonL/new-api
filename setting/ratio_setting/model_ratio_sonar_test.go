package ratio_setting

import "testing"

// These four entries previously read `1 / 1000 * USD`. That is an untyped
// integer constant expression, so the compiler folded it to 0 and the two large
// models were billed as free. The literal must stay a float.
//
// What this test pins is the division semantics, not a price claim. Perplexity's
// current price page lists Sonar / Sonar Pro / Sonar Reasoning Pro / Sonar Deep
// Research and no longer lists these Llama-era names, so the 0.5 / 0.1 values
// are the last published rates rather than a rate that can be re-verified
// against a live vendor page today. See docs/operations/pricing-maintenance.md,
// which records the same judgement ("发布方无现行价格，无法判定方向 / 保持现状").
//
// Consequence: this test does not assert that $1/M is the correct current price.
// It asserts the ratio is non-zero and still equals the documented last rate, so
// a future price decision must change this expectation deliberately instead of
// silently inheriting a number no one re-checked.
func TestSonarLargeModelRatioIsNotZero(t *testing.T) {
	cases := []struct {
		model string
		want  float64
	}{
		{model: "llama-3-sonar-large-32k-chat", want: 0.5},
		{model: "llama-3-sonar-large-32k-online", want: 0.5},
		{model: "llama-3-sonar-small-32k-chat", want: 0.1},
		{model: "llama-3-sonar-small-32k-online", want: 0.1},
	}
	for _, tc := range cases {
		got, ok := defaultModelRatio[tc.model]
		if !ok {
			t.Fatalf("%s missing from defaultModelRatio", tc.model)
		}
		// The regression this test exists for: a zero ratio bills the model free.
		if got == 0 {
			t.Fatalf("defaultModelRatio[%q] is zero: the model would be billed as free", tc.model)
		}
		if got != tc.want {
			t.Fatalf("defaultModelRatio[%q] = %v, want %v (documented last published rate)", tc.model, got, tc.want)
		}
	}
}

// The two families must stay proportional to their published 1:5 ratio. Guards
// against "fixing" one entry's division and leaving the sibling inconsistent.
func TestSonarLargeToSmallRatioIsFive(t *testing.T) {
	large, okLarge := defaultModelRatio["llama-3-sonar-large-32k-chat"]
	small, okSmall := defaultModelRatio["llama-3-sonar-small-32k-chat"]
	if !okLarge || !okSmall {
		t.Fatal("sonar chat entries missing from defaultModelRatio")
	}
	if small == 0 {
		t.Fatal("small sonar ratio is zero, cannot compute the ratio")
	}
	if got := large / small; got != 5 {
		t.Fatalf("large/small sonar ratio = %v, want 5", got)
	}
}
