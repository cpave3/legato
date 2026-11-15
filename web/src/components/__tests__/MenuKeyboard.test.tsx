import "@testing-library/jest-dom/vitest"
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { AgentMoreMenu } from '../AgentChatHeader'

it('selects an agent action with roving keys and returns to its trigger', async () => {
  const user = userEvent.setup()
  const select = vi.fn()
  render(<AgentMoreMenu entries={[{ label: 'Unavailable', disabled: true, onSelect: vi.fn() }, { label: 'Rename', onSelect: vi.fn() }, { label: 'Reload', onSelect: select }]} />)
  const trigger = screen.getByRole('button', { name: 'More actions' })
  await user.click(trigger)
  expect(screen.getByRole('menuitem', { name: 'Rename' })).toHaveFocus()
  await user.keyboard('{End}{Enter}')
  expect(select).toHaveBeenCalledOnce()
  expect(trigger).toHaveFocus()
  expect(screen.queryByRole('menu')).not.toBeInTheDocument()
})

import { ComposerModelMenu } from '../ComposerModelMenu'
it('chooses effort as a radio menu item and restores the model trigger', async () => {
  const user = userEvent.setup()
  const effort = vi.fn()
  render(<ComposerModelMenu models={[{ ref: 'alpha', isDefault: true }]} model="alpha" efforts={['low', 'high']} effort="low" onModel={vi.fn()} onEffort={effort} />)
  const trigger = screen.getByRole('button', { name: /alpha/ })
  await user.click(trigger)
  expect(screen.getByRole('menuitemradio', { name: 'low' })).toHaveAttribute('aria-checked', 'true')
  await user.keyboard('h{Enter}')
  expect(effort).toHaveBeenCalledWith('high')
  expect(trigger).toHaveFocus()
})

import { useState } from 'react'
import { Dropdown, DropdownItem } from '../ui/Dropdown'
it('opens a dropdown, navigates and dismisses with Tab', async () => {
  function Example() {
    const [open, setOpen] = useState(false)
    return <Dropdown open={open} onOpenChange={setOpen} trigger={<button onClick={() => setOpen(!open)}>Choose</button>}><DropdownItem>First</DropdownItem><DropdownItem>Second</DropdownItem></Dropdown>
  }
  const user = userEvent.setup()
  render(<Example />)
  const trigger = screen.getByRole('button', { name: 'Choose' })
  await user.click(trigger)
  expect(trigger).toHaveAttribute('aria-expanded', 'true')
  await user.keyboard('{ArrowDown}')
  expect(screen.getByRole('menuitem', { name: 'Second' })).toHaveFocus()
  await user.tab()
  expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  expect(trigger).toHaveFocus()
})

import { CardContextMenu } from '../board/CardContextMenu'
import type { BoardCard } from '../../lib/board-types'
it('uses a stacked workflow menu with Back on a phone and selects the workflow', async () => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })
  const user = userEvent.setup()
  const select = vi.fn()
  const workflow = { definition: { name: 'triage', title: 'Triage' }, taskInput: 'task' } as never
  function Example() {
    const [open, setOpen] = useState(false)
    return <><button onClick={() => setOpen(true)}>Card actions</button>{open && <CardContextMenu card={{ id: 't', title: 'Task' } as BoardCard} position={{ x: 300, y: 200 }} workflows={[workflow]} onClose={() => setOpen(false)} onAction={vi.fn()} onWorkflow={select} />}</>
  }
  render(<Example />)
  const trigger = screen.getByRole('button', { name: 'Card actions' })
  await user.click(trigger)
  await user.click(screen.getByRole('menuitem', { name: 'Workflows' }))
  expect(screen.queryByRole('menuitem', { name: 'Move' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('menuitem', { name: 'Back' }))
  expect(screen.getByRole('menuitem', { name: 'Workflows' })).toHaveFocus()
  await user.keyboard('{Enter}t{Enter}')
  expect(select).toHaveBeenCalledWith(workflow)
  expect(trigger).toHaveFocus()
})

it('opens from ArrowUp at the last action and does not reset focus on refresh', async () => {
  const user = userEvent.setup()
  const entries = [{ label: 'Rename', onSelect: vi.fn() }, { label: 'Reload', onSelect: vi.fn() }]
  const view = render(<AgentMoreMenu entries={entries} />)
  const trigger = screen.getByRole('button', { name: 'More actions' })
  trigger.focus()
  await user.keyboard('{ArrowUp}')
  expect(screen.getByRole('menuitem', { name: 'Reload' })).toHaveFocus()
  view.rerender(<AgentMoreMenu entries={[...entries]} />)
  expect(screen.getByRole('menuitem', { name: 'Reload' })).toHaveFocus()
  await user.keyboard('{Home}{Escape}')
  expect(trigger).toHaveFocus()
})

it.each([320, 390])('fits measured menu content in a %ipx wide, short viewport', async (width) => {
  const user = userEvent.setup()
  const originalWidth = window.innerWidth
  const originalHeight = window.innerHeight
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 180 })
  const bounds = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return this.getAttribute('role') === 'menu' ? new DOMRect(0, 0, 240, 164) : new DOMRect(width - 30, 150, 22, 22)
  })
  try {
    render(<AgentMoreMenu entries={Array.from({ length: 12 }, (_, i) => ({ label: `Action ${i}`, onSelect: vi.fn() }))} />)
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    const menu = screen.getByRole('menu')
    expect(Number.parseFloat(menu.style.left) + 240).toBeLessThanOrEqual(width - 8)
    expect(menu.style.top).toBe('8px')
    expect(menu.style.maxHeight).toBe('164px')
    expect(menu.style.overflowY).toBe('auto')
    await user.keyboard('{End}')
    expect(screen.getByRole('menuitem', { name: 'Action 11' })).toHaveFocus()
  } finally {
    bounds.mockRestore()
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalWidth })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: originalHeight })
  }
})

it('skips locked effort while retaining model choice and returning focus', async () => {
  const user = userEvent.setup()
  const model = vi.fn()
  const effort = vi.fn()
  render(<ComposerModelMenu models={[{ ref: 'alpha', isDefault: true }, { ref: 'beta', isDefault: false }]} model="alpha" efforts={['low', 'high']} effort="low" effortLocked="A turn is running" onModel={model} onEffort={effort} />)
  const trigger = screen.getByRole('button', { name: /alpha/ })
  await user.click(trigger)
  expect(screen.getByRole('menuitemradio', { name: 'high' })).toBeDisabled()
  await user.keyboard('{End}{Enter}')
  expect(model).toHaveBeenCalledWith('beta')
  expect(effort).not.toHaveBeenCalled()
  expect(trigger).toHaveFocus()
})
