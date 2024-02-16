package customhandlers_test

import (
	"bytes"
	"testing"

	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_OutputReader(t *testing.T) {
	tests := []struct {
		name             string
		outputReaderMock func(ctrl *gomock.Controller) *MockoutputReader
		serverRepoMock   func(ctrl *gomock.Controller) *MockserverRepo
		args             []string
		wantExitCode     int
		wantErr          string
	}{
		{
			name:         "no server id provided",
			args:         []string{},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "no server id provided",
		},
		{
			name:         "invalid server id, should be integer",
			args:         []string{"abc"},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "invalid server id, should be integer",
		},
		{
			name: "failed to get server",
			args: []string{"1"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(nil, assert.AnError)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "failed to get server",
		},
		{
			name: "server not found",
			args: []string{"1"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(nil, nil)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "server not found",
		},
		{
			name: "getter.GetOutput error",
			args: []string{"1"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(&domain.Server{}, nil)
				return m
			},
			outputReaderMock: func(ctrl *gomock.Controller) *MockoutputReader {
				m := NewMockoutputReader(ctrl)
				m.EXPECT().GetOutput(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.ErrorResult, assert.AnError)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "failed to get output",
		},
		{
			name: "success",
			args: []string{"1"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(&domain.Server{}, nil)
				return m
			},
			outputReaderMock: func(ctrl *gomock.Controller) *MockoutputReader {
				m := NewMockoutputReader(ctrl)
				m.EXPECT().GetOutput(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.SuccessResult, nil)
				return m
			},
			wantExitCode: int(domain.SuccessResult),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// ARRANGE
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var outputReaderMock *MockoutputReader
			var serverRepoMock *MockserverRepo

			if test.outputReaderMock != nil {
				outputReaderMock = test.outputReaderMock(ctrl)
			}
			if test.serverRepoMock != nil {
				serverRepoMock = test.serverRepoMock(ctrl)
			}

			or := customhandlers.NewOutputReader(
				outputReaderMock,
				serverRepoMock,
			)
			out := new(bytes.Buffer)

			// ACT
			result, err := or.Handle(nil, test.args, out, contracts.ExecutorOptions{})

			// ASSERT
			if test.wantErr == "" {
				assert.Equal(t, test.wantExitCode, result)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)
				assert.Equal(t, test.wantExitCode, result)
			}
		})
	}
}

func Test_CommandSender(t *testing.T) {
	tests := []struct {
		name              string
		commandSenderMock func(ctrl *gomock.Controller) *MockcommandSender
		serverRepoMock    func(ctrl *gomock.Controller) *MockserverRepo
		args              []string
		wantExitCode      int
		wantErr           string
	}{
		{
			name:         "not enough arguments",
			args:         []string{},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "not enough arguments",
		},
		{
			name:         "not enough arguments",
			args:         []string{"1"},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "not enough arguments",
		},
		{
			name:         "invalid server id, should be integer",
			args:         []string{"abc", "command"},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "invalid server id, should be integer",
		},
		{
			name: "failed to get server",
			args: []string{"1", "some", "command"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(nil, assert.AnError)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "failed to get server",
		},
		{
			name: "server not found",
			args: []string{"1", "some", "command"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(nil, nil)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "server not found",
		},
		{
			name: "sender error",
			args: []string{"1", "some", "command"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(&domain.Server{}, nil)
				return m
			},
			commandSenderMock: func(ctrl *gomock.Controller) *MockcommandSender {
				m := NewMockcommandSender(ctrl)
				m.EXPECT().SendInput(gomock.Any(), "some command", gomock.Any(), gomock.Any()).Return(domain.ErrorResult, assert.AnError)
				return m
			},
			wantExitCode: int(domain.ErrorResult),
			wantErr:      "failed to send command: assert.AnError general error for testing",
		},
		{
			name: "success",
			args: []string{"1", "some", "command", "with", "arguments"},
			serverRepoMock: func(ctrl *gomock.Controller) *MockserverRepo {
				m := NewMockserverRepo(ctrl)
				m.EXPECT().FindByID(gomock.Any(), 1).Return(&domain.Server{}, nil)
				return m
			},
			commandSenderMock: func(ctrl *gomock.Controller) *MockcommandSender {
				m := NewMockcommandSender(ctrl)
				m.EXPECT().SendInput(gomock.Any(), "some command with arguments", gomock.Any(), gomock.Any()).Return(domain.SuccessResult, nil)
				return m
			},
			wantExitCode: int(domain.SuccessResult),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// ARRANGE
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var commandSenderMock *MockcommandSender
			var serverRepoMock *MockserverRepo

			if test.commandSenderMock != nil {
				commandSenderMock = test.commandSenderMock(ctrl)
			}
			if test.serverRepoMock != nil {
				serverRepoMock = test.serverRepoMock(ctrl)
			}

			or := customhandlers.NewCommandSender(
				commandSenderMock,
				serverRepoMock,
			)
			out := new(bytes.Buffer)

			// ACT
			result, err := or.Handle(nil, test.args, out, contracts.ExecutorOptions{})

			// ASSERT
			if test.wantErr == "" {
				assert.Equal(t, test.wantExitCode, result)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)
				assert.Equal(t, test.wantExitCode, result)
			}
		})
	}
}
