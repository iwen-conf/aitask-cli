package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	codeProjectNotFound             = "PROJECT_NOT_FOUND"
	codeProjectAccessDenied         = "PROJECT_ACCESS_DENIED"
	codeAgentNotBoundToProject      = "AGENT_NOT_BOUND_TO_PROJECT"
	codeAgentTokenInvalid           = "AGENT_TOKEN_INVALID"
	codeAgentTokenExpired           = "AGENT_TOKEN_EXPIRED"
	codeTaskNotFound                = "TASK_NOT_FOUND"
	codeTaskNotEligibleForAgent     = "TASK_NOT_ELIGIBLE_FOR_AGENT"
	codeTaskAlreadyDelegated        = "TASK_ALREADY_DELEGATED"
	codeTaskNotAssignedCurrentAgent = "TASK_NOT_ASSIGNED_TO_CURRENT_AGENT"
	codeTaskActiveRunMismatch       = "TASK_ACTIVE_RUN_MISMATCH"
	codeTaskDependencyNotDone       = "TASK_DEPENDENCY_NOT_DONE"
	codeTaskStatusInvalid           = "TASK_STATUS_INVALID"
	codeRoomNotFound                = "ROOM_NOT_FOUND"
	codeRoomAccessDenied            = "ROOM_ACCESS_DENIED"
	codeRoomMessageTooLarge         = "ROOM_MESSAGE_TOO_LARGE"
	codeContextBudgetExceeded       = "CONTEXT_BUDGET_EXCEEDED"
	codeContextHandoffRequired      = "CONTEXT_HANDOFF_REQUIRED"
	codeHandoffNotFound             = "HANDOFF_NOT_FOUND"
	codeHandoffAlreadyConsumed      = "HANDOFF_ALREADY_CONSUMED"
	codeOpenVikingWriteFailed       = "OPENVIKING_WRITE_FAILED"
	codeOpenVikingReadFailed        = "OPENVIKING_READ_FAILED"
)

type errorGuide struct {
	message  string
	nextStep string
}

var errorGuides = map[string]errorGuide{
	codeProjectNotFound: {
		message:  "项目不存在，确认 `project_id` 是否正确。",
		nextStep: "aitask project info",
	},
	codeProjectAccessDenied: {
		message:  "当前 Token 没有该项目访问权限。",
		nextStep: "aitask whoami",
	},
	codeAgentNotBoundToProject: {
		message:  "目标 Agent 未绑定到项目。",
		nextStep: "调用 /api/projects/:projectId/agents/:agentId/bind 完成绑定",
	},
	codeAgentTokenInvalid: {
		message:  "Agent Token 无效。",
		nextStep: "aitask auth token import",
	},
	codeAgentTokenExpired: {
		message:  "Agent Token 已过期。",
		nextStep: "重新颁发 token 后执行 aitask auth token import",
	},
	codeTaskNotFound: {
		message:  "任务不存在。",
		nextStep: "aitask task inbox",
	},
	codeTaskNotEligibleForAgent: {
		message:  "当前 Agent 不具备执行该任务的资格或权限。",
		nextStep: "aitask whoami",
	},
	codeTaskAlreadyDelegated: {
		message:  "任务已被委托，不能重复委托。",
		nextStep: "aitask task detail <task_id>",
	},
	codeTaskNotAssignedCurrentAgent: {
		message:  "任务未分配给当前 Agent。",
		nextStep: "aitask task current",
	},
	codeTaskActiveRunMismatch: {
		message:  "运行实例不匹配，run_id 可能过期或已切换。",
		nextStep: "aitask task detail <task_id>",
	},
	codeTaskDependencyNotDone: {
		message:  "依赖任务未完成，当前任务不能开始。",
		nextStep: "aitask task detail <task_id>",
	},
	codeTaskStatusInvalid: {
		message:  "任务当前状态不允许执行该操作。",
		nextStep: "aitask task detail <task_id>",
	},
	codeRoomNotFound: {
		message:  "聊天室不存在。",
		nextStep: "aitask room join",
	},
	codeRoomAccessDenied: {
		message:  "当前身份没有聊天室访问权限。",
		nextStep: "aitask whoami",
	},
	codeRoomMessageTooLarge: {
		message:  "聊天室消息过长，请缩短后重试。",
		nextStep: "aitask room send \"<short message>\"",
	},
	codeContextBudgetExceeded: {
		message:  "上下文预算已超限。",
		nextStep: "aitask context handoff prepare",
	},
	codeContextHandoffRequired: {
		message:  "当前上下文状态要求先完成 handoff。",
		nextStep: "aitask context handoff prepare",
	},
	codeHandoffNotFound: {
		message:  "找不到指定 handoff。",
		nextStep: "aitask context handoff current",
	},
	codeHandoffAlreadyConsumed: {
		message:  "handoff 已被消费，不能重复 resume。",
		nextStep: "aitask context handoff current",
	},
	codeOpenVikingWriteFailed: {
		message:  "OpenViking 写入失败。",
		nextStep: "aitask memory write --from <file> --target decisions",
	},
	codeOpenVikingReadFailed: {
		message:  "OpenViking 读取失败。",
		nextStep: "aitask memory search \"<query>\" --refs-only",
	},
}

func enhanceCommandError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	code := strings.TrimSpace(apiErr.Code)
	if code == "" {
		return err
	}
	guide, ok := errorGuides[code]
	if !ok {
		return err
	}
	apiErr.Message = strings.TrimSpace(guide.message)
	if apiErr.Details == nil {
		apiErr.Details = map[string]any{}
	}
	if strings.TrimSpace(guide.nextStep) != "" {
		apiErr.Details["nextCommand"] = guide.nextStep
	}
	return apiErr
}

func writeCommandError(out io.Writer, err error) {
	if out == nil || err == nil {
		return
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		_, _ = fmt.Fprintf(out, "Error [%s]: %s\n", apiErr.Code, apiErr.Message)
		if next := mapString(apiErr.Details, "nextCommand"); next != "" {
			_, _ = fmt.Fprintf(out, "下一步: %s\n", next)
		}
		return
	}
	_, _ = fmt.Fprintf(out, "Error: %s\n", err)
}
