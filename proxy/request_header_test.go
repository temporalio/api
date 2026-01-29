package proxy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/workflowservice/v1"
)

// findHeader searches for a header key in the headers slice and returns its value
func findHeader(headers []string, key string) (string, bool) {
	for i := 0; i < len(headers); i += 2 {
		if i+1 < len(headers) && headers[i] == key {
			return headers[i+1], true
		}
	}
	return "", false
}

func TestExtractTemporalRequestHeaders(t *testing.T) {
	tests := []struct {
		name           string
		req            proto.Message
		expectedHeader string
		expectedValue  string
		expectNS       bool
	}{
		{
			name: "StartWorkflowExecutionRequest with workflow_id",
			req: &workflowservice.StartWorkflowExecutionRequest{
				Namespace:  "test-namespace",
				WorkflowId: "test-workflow-123",
			},
			expectedHeader: "temporal-resource-id",
			expectedValue:  "workflow:test-workflow-123",
			expectNS:       true,
		},
		{
			name: "GetWorkflowExecutionHistoryRequest with execution.workflow_id",
			req: &workflowservice.GetWorkflowExecutionHistoryRequest{
				Namespace: "test-namespace",
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "test-workflow-456",
				},
			},
			expectedHeader: "temporal-resource-id",
			expectedValue:  "workflow:test-workflow-456",
			expectNS:       true,
		},
		{
			name: "SignalWorkflowExecutionRequest with workflow_execution.workflow_id",
			req: &workflowservice.SignalWorkflowExecutionRequest{
				Namespace: "test-namespace",
				WorkflowExecution: &commonpb.WorkflowExecution{
					WorkflowId: "test-workflow-789",
				},
				SignalName: "test-signal",
			},
			expectedHeader: "temporal-resource-id",
			expectedValue:  "workflow:test-workflow-789",
			expectNS:       true,
		},
		{
			name: "DescribeWorkflowExecutionRequest with execution.workflow_id",
			req: &workflowservice.DescribeWorkflowExecutionRequest{
				Namespace: "test-namespace",
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "test-workflow-describe",
				},
			},
			expectedHeader: "temporal-resource-id",
			expectedValue:  "workflow:test-workflow-describe",
			expectNS:       true,
		},
		{
			name: "StartWorkflowExecutionRequest without namespace",
			req: &workflowservice.StartWorkflowExecutionRequest{
				WorkflowId: "test-workflow-no-ns",
			},
			expectedHeader: "temporal-resource-id",
			expectedValue:  "workflow:test-workflow-no-ns",
			expectNS:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers, err := ExtractTemporalRequestHeaders(context.Background(), ExtractHeadersOptions{
				Request: tt.req,
			})
			require.NoError(t, err)

			// Check resource-id header
			val, found := findHeader(headers, tt.expectedHeader)
			require.True(t, found, "Expected header %q not found", tt.expectedHeader)
			require.Equal(t, tt.expectedValue, val, "Header %q has wrong value", tt.expectedHeader)

			// Check namespace header
			nsVal, found := findHeader(headers, "temporal-namespace")
			if tt.expectNS {
				require.True(t, found, "Expected temporal-namespace header, but not found")
				require.Equal(t, "test-namespace", nsVal, "temporal-namespace has wrong value")
			} else {
				require.False(t, found, "Did not expect temporal-namespace header, but found %q", nsVal)
			}
		})
	}
}

func TestExtractTemporalRequestHeaders_NamespaceAlwaysIncluded(t *testing.T) {
	req := &workflowservice.StartWorkflowExecutionRequest{
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
	}

	headers, err := ExtractTemporalRequestHeaders(context.Background(), ExtractHeadersOptions{
		Request: req,
	})
	require.NoError(t, err)

	// Namespace should always be included in the headers
	nsVal, found := findHeader(headers, "temporal-namespace")
	require.True(t, found, "Expected temporal-namespace header, but not found")
	require.Equal(t, "test-namespace", nsVal)
}

func TestExtractTemporalRequestHeaders_EmptyWorkflowId(t *testing.T) {
	req := &workflowservice.StartWorkflowExecutionRequest{
		Namespace:  "test-namespace",
		WorkflowId: "",
	}

	headers, err := ExtractTemporalRequestHeaders(context.Background(), ExtractHeadersOptions{
		Request: req,
	})
	require.NoError(t, err)

	// Should not set temporal-resource-id if workflow_id is empty
	_, found := findHeader(headers, "temporal-resource-id")
	require.False(t, found, "Did not expect temporal-resource-id header for empty workflow_id")

	// Should still set namespace
	nsVal, found := findHeader(headers, "temporal-namespace")
	require.True(t, found, "Expected temporal-namespace header even with empty workflow_id")
	require.Equal(t, "test-namespace", nsVal)
}

func TestExtractTemporalRequestHeaders_SkipExistingHeaders(t *testing.T) {
	req := &workflowservice.StartWorkflowExecutionRequest{
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
	}

	existingMD := metadata.MD{}
	existingMD.Set("temporal-namespace", "existing-namespace")
	existingMD.Set("temporal-resource-id", "workflow:existing-workflow")

	headers, err := ExtractTemporalRequestHeaders(context.Background(), ExtractHeadersOptions{
		Request:          req,
		ExistingMetadata: existingMD,
	})
	require.NoError(t, err)

	// Should not add any headers since they already exist
	require.Empty(t, headers, "Expected no headers to be added when they already exist")
}

func TestExtractTemporalRequestHeaders_NilRequest(t *testing.T) {
	headers, err := ExtractTemporalRequestHeaders(context.Background(), ExtractHeadersOptions{
		Request: nil,
	})
	require.Error(t, err)
	require.Nil(t, headers)
	require.Equal(t, "request cannot be nil", err.Error())
}
