package common

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocateTopLevelJSONStringFindsEscapedModel(t *testing.T) {
	body := []byte(`{
  "future_before": {"model":"nested-ignored"},
  "model": "client-\\\"model",
  "input": [{"type":"reasoning","encrypted_content":{"ciphertext":"opaque"}}],
  "future_after": {"model":"also-ignored"}
}`)

	location, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")

	require.NoError(t, err)
	require.Equal(t, `client-\"model`, location.Value)
	require.Equal(t, []byte(`"client-\\\"model"`), body[location.Start:location.End])
}

func TestLocateTopLevelJSONStringAcceptsJSONSlashEscapesAndKeepsFinalModel(t *testing.T) {
	body := []byte(`{"model":"stale-model","input":[],"model":"https:\/\/example.test\/gpt-5.6-sol"}`)

	location, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")

	require.NoError(t, err)
	require.Equal(t, "https://example.test/gpt-5.6-sol", location.Value)
	require.Equal(t, []byte(`"https:\/\/example.test\/gpt-5.6-sol"`), body[location.Start:location.End])

	replay, err := NewTopLevelJSONStringReplacementReader(
		bytes.NewReader(body),
		int64(len(body)),
		location,
		"upstream-gpt-5.6-sol",
	)
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(
		t,
		[]byte(`{"model":"stale-model","input":[],"model":"upstream-gpt-5.6-sol"}`),
		replayed,
	)
}

func TestLocateTopLevelJSONStringUsesFinalModelAfterEarlierNull(t *testing.T) {
	body := []byte(`{"model":null,"input":[],"model":"gpt-5.6-sol"}`)

	location, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", location.Value)
	require.Equal(t, []byte(`"gpt-5.6-sol"`), body[location.Start:location.End])
}

func TestLocateTopLevelJSONStringRejectsFinalNonStringModel(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[],"model":null}`)

	_, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")

	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestTopLevelJSONStringReplacementReaderPreservesEverythingExceptModel(t *testing.T) {
	body := []byte(`{"future":{"model":"nested"},"model":"client-model","input":[{"type":"reasoning","encrypted_content":{"opaque":true}}],"tail":[1,2,3]}`)
	source := bytes.NewReader(body)
	location, err := LocateTopLevelJSONString(source, "model")
	require.NoError(t, err)

	replay, err := NewTopLevelJSONStringReplacementReader(source, int64(len(body)), location, "mapped-model-with-a-longer-name")
	require.NoError(t, err)

	got, err := io.ReadAll(replay)
	require.NoError(t, err)
	want := bytes.Replace(body, []byte(`"client-model"`), []byte(`"mapped-model-with-a-longer-name"`), 1)
	require.Equal(t, want, got)
	require.Equal(t, int64(len(want)), replay.ContentLength())
}

func TestTopLevelJSONStringReplacementReaderReplaysWithOneByteBuffer(t *testing.T) {
	body := []byte(`{"future":{"model":"nested"},"model":"client-model","input":[{"type":"reasoning","encrypted_content":"opaque"}],"tail":true}`)
	location, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")
	require.NoError(t, err)

	replay, err := NewTopLevelJSONStringReplacementReader(
		bytes.NewReader(body),
		int64(len(body)),
		location,
		"mapped-model-with-a-longer-name",
	)
	require.NoError(t, err)

	buffer := make([]byte, 1)
	var replayed []byte
	for {
		count, readErr := replay.Read(buffer)
		replayed = append(replayed, buffer[:count]...)
		if readErr == io.EOF {
			break
		}
		require.NoError(t, readErr)
	}

	want := bytes.Replace(body, []byte(`"client-model"`), []byte(`"mapped-model-with-a-longer-name"`), 1)
	require.Equal(t, want, replayed)
	require.Equal(t, int64(len(want)), replay.ContentLength())
}

func TestLocateTopLevelJSONStringRejectsOversizedModel(t *testing.T) {
	modelName := strings.Repeat("m", maxTopLevelJSONStringCaptureBytes+1)
	body := []byte(`{"model":"` + modelName + `","input":[]}`)

	_, err := LocateTopLevelJSONString(bytes.NewReader(body), "model")

	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestLocateTopLevelJSONStringRejectsMissingAndInvalidModel(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "missing field",
			body: `{"input":[]}`,
			want: ErrTopLevelJSONStringNotFound,
		},
		{
			name: "non string field",
			body: `{"model":123,"input":[]}`,
			want: nil,
		},
		{
			name: "invalid trailing content",
			body: `{"model":"x"} trailing`,
			want: nil,
		},
		{
			name: "trailing comma",
			body: `{"model":"x",}`,
			want: nil,
		},
		{
			name: "nested trailing comma",
			body: `{"model":"x","future":{"nested":true,}}`,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LocateTopLevelJSONString(strings.NewReader(tc.body), "model")
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
}

func TestSizedReaderReportsUnexpectedEOF(t *testing.T) {
	reader, err := NewSizedReader(bytes.NewReader([]byte("short")), 6)
	require.NoError(t, err)

	_, err = io.ReadAll(reader)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

type noProgressReadSeeker struct{}

func (noProgressReadSeeker) Read(_ []byte) (int, error) {
	return 0, nil
}

func (noProgressReadSeeker) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

func TestSizedReaderStopsAfterRepeatedEmptyReads(t *testing.T) {
	reader, err := NewSizedReader(noProgressReadSeeker{}, 1)
	require.NoError(t, err)

	_, err = reader.Read(make([]byte, 1))

	require.ErrorIs(t, err, io.ErrNoProgress)
}

func TestTopLevelJSONStringReplacementReaderRejectsInvalidLocation(t *testing.T) {
	_, err := NewTopLevelJSONStringReplacementReader(bytes.NewReader([]byte(`{"model":"x"}`)), 13, TopLevelJSONStringLocation{Start: 9, End: 2}, "mapped")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrTopLevelJSONStringNotFound))
}
