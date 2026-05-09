/**
 * TypeScript mirror of `agent-room-envelope.schema.json`.
 * Keep this file in sync with docs/API and backend room envelope output.
 */

export type AgentRoomEventType =
  | 'room.connected'
  | 'room.member_online'
  | 'room.member_offline'
  | 'room.message'
  | 'task.updated'
  | 'context.handoff_created'
  | 'system.notice'

export type AgentRoomSenderType = 'operator' | 'agent' | 'system'
export type AgentRoomAgentType = 'claude-code' | 'codex' | 'gemini' | 'system'

export interface AgentRoomSender {
  type: AgentRoomSenderType
  operatorLabel?: string
  agentId?: string
  agentType?: AgentRoomAgentType
}

export interface AgentRoomEnvelope<Payload = Record<string, unknown>> {
  eventId: string
  eventType: AgentRoomEventType
  projectId: string
  roomId: string
  sender: AgentRoomSender
  payload: Payload
  createdAt: string
}
