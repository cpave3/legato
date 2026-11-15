import '@testing-library/jest-dom/vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { AgentActionMenu } from '../AgentActionMenu'
import { messageWorker } from '../../lib/swarm'
vi.mock('../../hooks/useServer', () => ({ useServer: () => ({ baseUrl: '' }) }))
vi.mock('../../hooks/useToast', () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock('../../lib/swarm', () => ({ messageWorker: vi.fn().mockResolvedValue(undefined), closeWorker: vi.fn(), finishSwarm: vi.fn() }))
it('opens Send message by keyboard and sends to the same worker, restoring Actions', async () => {
  const user = userEvent.setup()
  render(<AgentActionMenu agentTaskId="worker" parentTaskId="parent" subtaskId="sub" role="worker" />)
  const trigger = screen.getByRole('button', { name: 'Actions' })
  await user.click(trigger)
  expect(screen.getByRole('menuitem', { name: 'Send message' })).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(screen.getByRole('dialog', { name: 'Send Message' })).toBeInTheDocument()
  await user.type(screen.getByRole('textbox', { name: 'Message' }), 'Please check')
  await user.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(messageWorker).toHaveBeenCalledWith('', 'worker', 'Please check'))
  expect(trigger).toHaveFocus()
})
