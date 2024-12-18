package yunikorn

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/G-Research/yunikorn-scheduler-interface/lib/go/si"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/G-Research/unicorn-history-server/internal/database/repository"
)

func TestFetchEventStream(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRepository := repository.NewMockRepository(mockCtrl)
	eventRepository := repository.NewInMemoryEventRepository()
	mockYunikornClient := NewMockClient(mockCtrl)
	mockYunikornClient.EXPECT().GetEventStream(gomock.Any()).DoAndReturn(
		func(ctx context.Context) (*http.Response, error) {
			// Create a pipe to simulate the server streaming response
			reader, writer := io.Pipe()

			// Write events to the writer in a separate goroutine
			go func() {
				defer func() { _ = writer.Close() }()
				time.Sleep(50 * time.Millisecond) // Simulate streaming delay
				events := []*si.EventRecord{
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_ADD},
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_ADD},
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_SET},
				}
				enc := json.NewEncoder(writer)
				for _, event := range events {
					if err := enc.Encode(event); err != nil {
						if err = writer.CloseWithError(err); err != nil {
							t.Errorf("error closing writer: %v", err)
						}
						return
					}
					time.Sleep(50 * time.Millisecond) // Simulate streaming delay
				}
			}()

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       reader,
			}, nil
		},
	)

	service := Service{
		repo:            mockRepository,
		eventRepository: eventRepository,
		client:          mockYunikornClient,
		eventHandler:    noopEventHandler,
	}

	// Start the ProcessEvents function in a separate goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	t.Cleanup(cancel)

	go func() {
		if err := service.ProcessEvents(ctx); err != nil {
			t.Errorf("error processing events: %v", err)
		}
	}()

	assert.Eventually(t, func() bool {
		eventCounts, err := service.eventRepository.Counts(ctx)
		if err != nil {
			t.Fatalf("error getting event counts: %v", err)
		}
		expectedKey1 := fmt.Sprintf("%s-%s", si.EventRecord_APP.String(), si.EventRecord_ADD.String())
		expectedKey2 := fmt.Sprintf("%s-%s", si.EventRecord_APP.String(), si.EventRecord_SET.String())
		return eventCounts[expectedKey1] == 2 && eventCounts[expectedKey2] == 1
	}, 1*time.Second, 50*time.Millisecond)
}

func TestEventRepositorySafety(t *testing.T) {
	ctx := context.Background()
	eventRepository := repository.NewInMemoryEventRepository()

	// Start a number of goroutines and randomly write to the event repository, to
	// verify safety of using the underlying map when returning results from its
	// Counts() method, and the returned map might be read concurrently.
	for n := 0; n < 10; n++ {
		go func() {
			for p := 0; p < 200; p++ {
				for _, ev := range []*si.EventRecord{
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_ADD},
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_ADD},
					{Type: si.EventRecord_APP, EventChangeType: si.EventRecord_SET},
				} {
					n, err := rand.Int(rand.Reader, big.NewInt(5))
					assert.NoError(t, err)
					time.Sleep(time.Duration(n.Int64()) * time.Millisecond)

					err = eventRepository.Record(ctx, ev)
					assert.NoError(t, err)
				}
			}
		}()
	}

	assert.Eventually(t, func() bool {
		eventCounts, err := eventRepository.Counts(ctx)
		if err != nil {
			t.Fatalf("error getting event counts: %v", err)
		}
		appAdds := fmt.Sprintf("%s-%s", si.EventRecord_APP.String(), si.EventRecord_ADD.String())
		appSets := fmt.Sprintf("%s-%s", si.EventRecord_APP.String(), si.EventRecord_SET.String())
		return eventCounts[appAdds] == 4000 && eventCounts[appSets] == 2000
	}, 2*time.Second, 5*time.Millisecond)
}

func TestProcessStreamResponse(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedErr   error
		expectedType  si.EventRecord_Type
		expectedEvent si.EventRecord_ChangeType
		expectedCount int
	}{
		{
			name:          "Valid Event",
			input:         `{"type": 2, "eventChangeType": 2}` + "\n",
			expectedErr:   nil,
			expectedType:  si.EventRecord_APP,
			expectedEvent: si.EventRecord_ADD,
			expectedCount: 1,
		},
		{
			name:        "Invalid JSON",
			input:       `{"type": 2, "eventChangeType": 2` + "\n", // Invalid JSON (missing closing brace)
			expectedErr: errors.New("could not unmarshal event from stream"),
		},
		{
			name:          "Empty Input",
			input:         "",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				eventRepository: repository.NewInMemoryEventRepository(),
				eventHandler:    noopEventHandler,
			}

			err := service.processStreamResponse(context.Background(), []byte(tt.input))

			if tt.expectedErr != nil {
				assert.ErrorContains(t, err, tt.expectedErr.Error())
			} else {
				if err != nil {
					t.Errorf("expected no error; got '%v'", err)
				}

				eventCounts, err := service.eventRepository.Counts(context.Background())
				if err != nil {
					t.Fatalf("error getting event counts: %v", err)
				}
				expectedKey := fmt.Sprintf("%s-%s", tt.expectedType.String(), tt.expectedEvent.String())
				assert.Equal(t, tt.expectedCount, eventCounts[expectedKey])
			}
		})
	}
}

func noopEventHandler(ctx context.Context, event *si.EventRecord) error {
	return nil
}
