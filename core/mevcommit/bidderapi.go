package mevcommit

import (
	"context"
	"fmt"

	pb "github.com/primev/mev-commit/p2p/gen/go/bidderapi/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// GetMinDeposit retrieves the minimum deposit required for bidding using mev-commit bidder api.
func (b *Bidder) GetMinDeposit() (*pb.DepositResponse, error) {
	ctx := context.Background()
	response, err := b.client.GetMinDeposit(ctx, &pb.EmptyMessage{})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// DepositMinBidAmount deposits the minimum bid amount into the bidding window using mev-commit bidder api.
func (b *Bidder) DepositMinBidAmount() (int64, error) {
	minDepositResponse, err := b.GetMinDeposit()
	if err != nil {
		return 0, fmt.Errorf("failed to get minimum deposit: %w", err)
	}

	minDepositAmount := minDepositResponse.Amount
	depositRequest := &pb.DepositRequest{
		Amount: minDepositAmount,
	}

	ctx := context.Background()
	response, err := b.client.Deposit(ctx, depositRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to deposit funds: %w", err)
	}

	windowNumber := int64(response.WindowNumber.Value)
	fmt.Printf("Deposited minimum bid amount successfully into window number: %v\n", windowNumber)
	return windowNumber, nil
}

// WithdrawFunds withdraws the deposited funds from the specified bidding window. using mev-commit bidder api
func (b *Bidder) WithdrawFunds(windowNumber int64) error {
	withdrawRequest := &pb.WithdrawRequest{
		WindowNumber: wrapperspb.UInt64(uint64(windowNumber)),
	}

	ctx := context.Background()
	response, err := b.client.Withdraw(ctx, withdrawRequest)
	if err != nil {
		return fmt.Errorf("failed to withdraw funds: %w", err)
	}

	fmt.Printf("Withdraw successful: %v\n", response)
	return nil
}