# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [aitask/v1/common.proto](#aitask_v1_common-proto)
    - [AgentIdentity](#aitask-v1-AgentIdentity)
    - [ArtifactRef](#aitask-v1-ArtifactRef)
    - [ContextRef](#aitask-v1-ContextRef)
    - [Error](#aitask-v1-Error)
    - [NextAction](#aitask-v1-NextAction)
    - [ProjectRef](#aitask-v1-ProjectRef)
    - [SessionRef](#aitask-v1-SessionRef)
    - [SkillRef](#aitask-v1-SkillRef)
  
- [aitask/v1/agent.proto](#aitask_v1_agent-proto)
    - [WhoAmIRequest](#aitask-v1-WhoAmIRequest)
    - [WhoAmIResponse](#aitask-v1-WhoAmIResponse)
  
    - [AgentService](#aitask-v1-AgentService)
  
- [aitask/v1/context.proto](#aitask_v1_context-proto)
    - [AgentRun](#aitask-v1-AgentRun)
    - [ContextBudget](#aitask-v1-ContextBudget)
    - [CreateHandoffRequest](#aitask-v1-CreateHandoffRequest)
    - [CreateHandoffResponse](#aitask-v1-CreateHandoffResponse)
    - [GetCurrentHandoffRequest](#aitask-v1-GetCurrentHandoffRequest)
    - [GetCurrentHandoffResponse](#aitask-v1-GetCurrentHandoffResponse)
    - [ReportRequest](#aitask-v1-ReportRequest)
    - [ReportResponse](#aitask-v1-ReportResponse)
  
    - [ContextService](#aitask-v1-ContextService)
  
- [aitask/v1/bootstrap.proto](#aitask_v1_bootstrap-proto)
    - [BootstrapRequest](#aitask-v1-BootstrapRequest)
    - [BootstrapResponse](#aitask-v1-BootstrapResponse)
    - [RoomSnapshot](#aitask-v1-RoomSnapshot)
  
    - [BootstrapService](#aitask-v1-BootstrapService)
  
- [aitask/v1/task.proto](#aitask_v1_task-proto)
    - [GetCurrentTaskRequest](#aitask-v1-GetCurrentTaskRequest)
    - [GetCurrentTaskResponse](#aitask-v1-GetCurrentTaskResponse)
    - [StartTaskRequest](#aitask-v1-StartTaskRequest)
    - [StartTaskResponse](#aitask-v1-StartTaskResponse)
    - [SubmitTaskRequest](#aitask-v1-SubmitTaskRequest)
    - [SubmitTaskResponse](#aitask-v1-SubmitTaskResponse)
    - [Task](#aitask-v1-Task)
    - [TaskDelegation](#aitask-v1-TaskDelegation)
  
    - [TaskService](#aitask-v1-TaskService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="aitask_v1_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## aitask/v1/common.proto



<a name="aitask-v1-AgentIdentity"></a>

### AgentIdentity



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_id | [string](#string) |  |  |
| agent_type | [string](#string) |  |  |
| role | [string](#string) |  |  |
| scopes | [string](#string) | repeated |  |
| allowed_projects | [string](#string) | repeated |  |






<a name="aitask-v1-ArtifactRef"></a>

### ArtifactRef



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| artifact_type | [string](#string) |  |  |
| uri | [string](#string) |  |  |






<a name="aitask-v1-ContextRef"></a>

### ContextRef



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| uri | [string](#string) |  |  |
| title | [string](#string) |  |  |
| estimated_tokens | [int32](#int32) |  |  |






<a name="aitask-v1-Error"></a>

### Error



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |
| message | [string](#string) |  |  |
| retriable | [bool](#bool) |  |  |






<a name="aitask-v1-NextAction"></a>

### NextAction



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| message | [string](#string) |  |  |
| command | [string](#string) |  |  |






<a name="aitask-v1-ProjectRef"></a>

### ProjectRef



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| status | [string](#string) |  |  |






<a name="aitask-v1-SessionRef"></a>

### SessionRef



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| status | [string](#string) |  |  |






<a name="aitask-v1-SkillRef"></a>

### SkillRef



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| source | [string](#string) |  |  |
| version | [string](#string) |  |  |





 

 

 

 



<a name="aitask_v1_agent-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## aitask/v1/agent.proto



<a name="aitask-v1-WhoAmIRequest"></a>

### WhoAmIRequest







<a name="aitask-v1-WhoAmIResponse"></a>

### WhoAmIResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity | [AgentIdentity](#aitask-v1-AgentIdentity) |  |  |





 

 

 


<a name="aitask-v1-AgentService"></a>

### AgentService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| WhoAmI | [WhoAmIRequest](#aitask-v1-WhoAmIRequest) | [WhoAmIResponse](#aitask-v1-WhoAmIResponse) |  |

 



<a name="aitask_v1_context-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## aitask/v1/context.proto



<a name="aitask-v1-AgentRun"></a>

### AgentRun



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| run_id | [string](#string) |  |  |
| context_budget | [ContextBudget](#aitask-v1-ContextBudget) |  |  |






<a name="aitask-v1-ContextBudget"></a>

### ContextBudget



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| max_context_tokens | [int32](#int32) |  |  |
| estimated_used_tokens | [int32](#int32) |  |  |
| state | [string](#string) |  |  |
| usage_ratio | [double](#double) |  |  |






<a name="aitask-v1-CreateHandoffRequest"></a>

### CreateHandoffRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |
| task_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |
| handoff_markdown | [string](#string) |  |  |






<a name="aitask-v1-CreateHandoffResponse"></a>

### CreateHandoffResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| handoff_id | [string](#string) |  |  |
| openviking_uri | [string](#string) |  |  |
| next_action | [NextAction](#aitask-v1-NextAction) |  |  |






<a name="aitask-v1-GetCurrentHandoffRequest"></a>

### GetCurrentHandoffRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |






<a name="aitask-v1-GetCurrentHandoffResponse"></a>

### GetCurrentHandoffResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| handoff_id | [string](#string) |  |  |
| task_id | [string](#string) |  |  |
| summary | [string](#string) |  |  |
| handoff_markdown | [string](#string) |  |  |
| context_refs | [ContextRef](#aitask-v1-ContextRef) | repeated |  |






<a name="aitask-v1-ReportRequest"></a>

### ReportRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |
| run_id | [string](#string) |  |  |
| reported_input_tokens | [int32](#int32) |  |  |
| reported_output_tokens | [int32](#int32) |  |  |
| max_context_tokens | [int32](#int32) |  |  |
| source | [string](#string) |  |  |






<a name="aitask-v1-ReportResponse"></a>

### ReportResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| budget | [ContextBudget](#aitask-v1-ContextBudget) |  |  |
| warnings | [string](#string) | repeated |  |
| next_action | [NextAction](#aitask-v1-NextAction) |  |  |





 

 

 


<a name="aitask-v1-ContextService"></a>

### ContextService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Report | [ReportRequest](#aitask-v1-ReportRequest) | [ReportResponse](#aitask-v1-ReportResponse) |  |
| CreateHandoff | [CreateHandoffRequest](#aitask-v1-CreateHandoffRequest) | [CreateHandoffResponse](#aitask-v1-CreateHandoffResponse) |  |
| GetCurrentHandoff | [GetCurrentHandoffRequest](#aitask-v1-GetCurrentHandoffRequest) | [GetCurrentHandoffResponse](#aitask-v1-GetCurrentHandoffResponse) |  |

 



<a name="aitask_v1_bootstrap-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## aitask/v1/bootstrap.proto



<a name="aitask-v1-BootstrapRequest"></a>

### BootstrapRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |






<a name="aitask-v1-BootstrapResponse"></a>

### BootstrapResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity | [AgentIdentity](#aitask-v1-AgentIdentity) |  |  |
| project | [ProjectRef](#aitask-v1-ProjectRef) |  |  |
| session | [SessionRef](#aitask-v1-SessionRef) |  |  |
| run | [AgentRun](#aitask-v1-AgentRun) |  |  |
| context_refs | [ContextRef](#aitask-v1-ContextRef) | repeated |  |
| room | [RoomSnapshot](#aitask-v1-RoomSnapshot) |  |  |
| next_action | [NextAction](#aitask-v1-NextAction) |  |  |






<a name="aitask-v1-RoomSnapshot"></a>

### RoomSnapshot



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| room_id | [string](#string) |  |  |
| recent_summary | [string](#string) |  |  |
| unread_mentions | [int32](#int32) |  |  |





 

 

 


<a name="aitask-v1-BootstrapService"></a>

### BootstrapService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Bootstrap | [BootstrapRequest](#aitask-v1-BootstrapRequest) | [BootstrapResponse](#aitask-v1-BootstrapResponse) |  |

 



<a name="aitask_v1_task-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## aitask/v1/task.proto



<a name="aitask-v1-GetCurrentTaskRequest"></a>

### GetCurrentTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |






<a name="aitask-v1-GetCurrentTaskResponse"></a>

### GetCurrentTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task | [Task](#aitask-v1-Task) |  |  |
| next_action | [NextAction](#aitask-v1-NextAction) |  |  |






<a name="aitask-v1-StartTaskRequest"></a>

### StartTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |
| task_id | [string](#string) |  |  |
| run_id | [string](#string) |  |  |






<a name="aitask-v1-StartTaskResponse"></a>

### StartTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task_id | [string](#string) |  |  |
| status | [string](#string) |  |  |
| active_run_id | [string](#string) |  |  |
| started_at | [string](#string) |  |  |






<a name="aitask-v1-SubmitTaskRequest"></a>

### SubmitTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| project_id | [string](#string) |  |  |
| task_id | [string](#string) |  |  |
| run_id | [string](#string) |  |  |
| result_markdown | [string](#string) |  |  |
| artifacts | [ArtifactRef](#aitask-v1-ArtifactRef) | repeated |  |






<a name="aitask-v1-SubmitTaskResponse"></a>

### SubmitTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task_id | [string](#string) |  |  |
| status | [string](#string) |  |  |
| next_action | [NextAction](#aitask-v1-NextAction) |  |  |






<a name="aitask-v1-Task"></a>

### Task



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task_id | [string](#string) |  |  |
| project_id | [string](#string) |  |  |
| title | [string](#string) |  |  |
| status | [string](#string) |  |  |
| assignee_agent_id | [string](#string) |  |  |
| assignee_agent_type | [string](#string) |  |  |
| active_run_id | [string](#string) |  |  |
| last_heartbeat_at | [string](#string) |  |  |
| delegation | [TaskDelegation](#aitask-v1-TaskDelegation) |  |  |
| required_skills | [SkillRef](#aitask-v1-SkillRef) | repeated |  |
| required_model | [string](#string) |  |  |
| output_contract | [string](#string) |  |  |
| priority | [int32](#int32) |  |  |






<a name="aitask-v1-TaskDelegation"></a>

### TaskDelegation



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| delegated_by_type | [string](#string) |  |  |
| delegated_by_operator_label | [string](#string) |  |  |
| delegated_by_agent_id | [string](#string) |  |  |
| delegated_at | [string](#string) |  |  |





 

 

 


<a name="aitask-v1-TaskService"></a>

### TaskService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetCurrentTask | [GetCurrentTaskRequest](#aitask-v1-GetCurrentTaskRequest) | [GetCurrentTaskResponse](#aitask-v1-GetCurrentTaskResponse) |  |
| StartTask | [StartTaskRequest](#aitask-v1-StartTaskRequest) | [StartTaskResponse](#aitask-v1-StartTaskResponse) |  |
| SubmitTask | [SubmitTaskRequest](#aitask-v1-SubmitTaskRequest) | [SubmitTaskResponse](#aitask-v1-SubmitTaskResponse) |  |

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

