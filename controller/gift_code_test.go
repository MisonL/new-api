package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestCanViewGiftCodeAfterReceived(t *testing.T) {
	giftCode := &model.GiftCode{
		CreatorUserId:  1,
		ReceivedUserId: 2,
	}

	require.True(t, canViewGiftCode(giftCode, 1))
	require.True(t, canViewGiftCode(giftCode, 2))
	require.False(t, canViewGiftCode(giftCode, 3))
}

func TestCanViewGiftCodeBeforeReceived(t *testing.T) {
	require.True(t, canViewGiftCode(&model.GiftCode{CreatorUserId: 1}, 2))
	require.True(t, canViewGiftCode(&model.GiftCode{CreatorUserId: 1, ReceiverUserId: 2}, 2))
	require.False(t, canViewGiftCode(&model.GiftCode{CreatorUserId: 1, ReceiverUserId: 2}, 3))
}
