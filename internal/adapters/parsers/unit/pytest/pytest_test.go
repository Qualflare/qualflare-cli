package pytest

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestPytestParserDefaultRetryCount(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="1">
        <testcase name="test_example" classname="test_module">
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected at least one case")
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

// BUG-38: the parser read the nonexistent attribute `skips` instead of pytest's
// real `skipped`, so a skipped test was counted as passed (Skipped=0, Passed
// inflated). A skipped case must roll up as skipped, never passed.
func TestPytestParser_SkippedNotCountedAsPassed(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="2" failures="0" errors="0" skipped="1">
        <testcase name="test_ok" classname="test_module"></testcase>
        <testcase name="test_skip" classname="test_module">
            <skipped message="not applicable"/>
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.Skipped != 1 {
		t.Errorf("expected Skipped == 1, got %d", suite.Skipped)
	}
	if suite.Passed != 1 {
		t.Errorf("expected Passed == 1 (not inflated by skipped), got %d", suite.Passed)
	}
	if suite.TotalTests != 2 {
		t.Errorf("expected TotalTests == 2, got %d", suite.TotalTests)
	}
}

func TestPytestParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPytestParser_MalformedXML(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("<not valid xml"))
	if err == nil {
		t.Error("expected error for malformed XML")
	}
}

// Mechanism C's fallback: a <property name="shard" value="..."/> (as written by a
// pytest-xdist conftest calling record_property("shard", ...)) sets ShardIndex. Mirrors
// junitxml's TestConvertTestCase_ShardPropertyFallback, since this parser is fully
// independent of junitxml and needs its own coverage of the same edge cases.
func TestPytestConvertTestCase_ShardPropertyFallback(t *testing.T) {
	tests := []struct {
		name      string
		props     []Property
		wantShard *int
	}{
		{"valid integer", []Property{{Name: "shard", Value: "2"}}, domain.IntPtr(2)},
		{"non-numeric value", []Property{{Name: "shard", Value: "not-a-number"}}, nil},
		{"empty value", []Property{{Name: "shard", Value: ""}}, nil},
		{"whitespace-padded value", []Property{{Name: "shard", Value: " 5 "}}, domain.IntPtr(5)},
		{"no shard property", []Property{{Name: "browser", Value: "chrome"}}, nil},
		{"no properties at all", nil, nil},
		// Same 32-bit bound as junitxml: an unstorable value is skipped, never emitted.
		{"zero", []Property{{Name: "shard", Value: "0"}}, domain.IntPtr(0)},
		{"negative value", []Property{{Name: "shard", Value: "-5"}}, nil},
		{"int32 max is still accepted", []Property{{Name: "shard", Value: "2147483647"}}, domain.IntPtr(2147483647)},
		{"one past int32 max", []Property{{Name: "shard", Value: "2147483648"}}, nil},
		{"int64 max", []Property{{Name: "shard", Value: "9223372036854775807"}}, nil},
		{"overflows any int", []Property{{Name: "shard", Value: "999999999999999999999999"}}, nil},
	}

	parser := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.convertTestCase(TestCase{Name: "t", Properties: tt.props})
			switch {
			case tt.wantShard == nil && got.ShardIndex != nil:
				t.Errorf("ShardIndex = %d, want nil", *got.ShardIndex)
			case tt.wantShard != nil && got.ShardIndex == nil:
				t.Errorf("ShardIndex = nil, want %d", *tt.wantShard)
			case tt.wantShard != nil && *got.ShardIndex != *tt.wantShard:
				t.Errorf("ShardIndex = %d, want %d", *got.ShardIndex, *tt.wantShard)
			}
		})
	}
}

// End-to-end coverage through Parse (not just convertTestCase directly) confirms the
// fallback is reachable via real pytest-xdist XML output, matching how record_property
// actually serializes: as a <properties><property> child of <testcase>.
func TestPytestParser_ShardPropertyFallbackViaXML(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="1">
        <testcase name="test_example" classname="test_module">
            <properties>
                <property name="shard" value="3"/>
            </properties>
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	testCase := suite.Cases[0]
	if testCase.ShardIndex == nil {
		t.Fatal("expected ShardIndex to be set, got nil")
	}
	if *testCase.ShardIndex != 3 {
		t.Errorf("ShardIndex = %d, want 3", *testCase.ShardIndex)
	}
	// The raw property must also still reach Properties (this parser's merge loop
	// already did this correctly before the fallback was added).
	if testCase.Properties["shard"] != "3" {
		t.Errorf("Properties[shard] = %q, want %q", testCase.Properties["shard"], "3")
	}
}
