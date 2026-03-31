package app

import (
	"testing"
)

// ── calculateCheckinReward edge cases ─────────────────────────────────────

func TestCalculateCheckinReward_Day1(t *testing.T) {
	reward := calculateCheckinReward(1)
	if reward != 0.10 {
		t.Errorf("reward for day 1 = %f, want 0.10", reward)
	}
}

func TestCalculateCheckinReward_Day7(t *testing.T) {
	// Day 7: streakTier = 7/7 = 1, multiplier = 1.5, reward = 0.15
	reward := calculateCheckinReward(7)
	expected := 0.15
	if reward < expected-0.001 || reward > expected+0.001 {
		t.Errorf("reward for day 7 = %f, want ~%f", reward, expected)
	}
}

func TestCalculateCheckinReward_Day14(t *testing.T) {
	// Day 14: streakTier = 14/7 = 2, multiplier = 1.5*1.5 = 2.25
	reward := calculateCheckinReward(14)
	expected := 0.10 * 1.5 * 1.5
	if reward != expected {
		t.Errorf("reward for day 14 = %f, want %f", reward, expected)
	}
}

func TestCalculateCheckinReward_Day21(t *testing.T) {
	// Day 21: streakTier = 21/7 = 3, multiplier = 1.5^3 = 3.375
	reward := calculateCheckinReward(21)
	expected := 0.10 * 1.5 * 1.5 * 1.5
	if reward != expected {
		t.Errorf("reward for day 21 = %f, want %f", reward, expected)
	}
}

func TestCalculateCheckinReward_Day365_CappedAtMax(t *testing.T) {
	// Day 365: streakTier = 52, multiplier is astronomical but capped at 1.0.
	reward := calculateCheckinReward(365)
	if reward != checkinMaxReward {
		t.Errorf("reward for day 365 = %f, want %f (max cap)", reward, checkinMaxReward)
	}
}

func TestCalculateCheckinReward_Day6_NoBonus(t *testing.T) {
	// Day 6: streakTier = 6/7 = 0, no bonus.
	reward := calculateCheckinReward(6)
	if reward != 0.10 {
		t.Errorf("reward for day 6 = %f, want 0.10 (no bonus yet)", reward)
	}
}

func TestCalculateCheckinReward_Day28_DoubleBonus(t *testing.T) {
	// Day 28: streakTier = 28/7 = 4, multiplier = 1.5^4 = 5.0625
	reward := calculateCheckinReward(28)
	expected := 0.10 * 1.5 * 1.5 * 1.5 * 1.5
	if expected > checkinMaxReward {
		expected = checkinMaxReward
	}
	if reward != expected {
		t.Errorf("reward for day 28 = %f, want %f", reward, expected)
	}
}

func TestCalculateCheckinReward_Day0(t *testing.T) {
	// Edge case: 0 consecutive days (shouldn't normally happen).
	reward := calculateCheckinReward(0)
	if reward != 0.10 {
		t.Errorf("reward for day 0 = %f, want 0.10", reward)
	}
}
