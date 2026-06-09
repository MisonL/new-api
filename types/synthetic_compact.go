package types

// SyntheticCompactStateScope defines identifiers used to validate synthetic compact state reuse.
// It is an in-process parameter object and is not serialized directly.
type SyntheticCompactStateScope struct {
	UserID      int
	TokenID     int
	Group       string
	Model       string
	ChannelID   int
	ChannelType int
}
