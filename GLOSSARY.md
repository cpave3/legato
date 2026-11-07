# Legato

A board of tasks and the AI coding agents that work on them, run as one or more Legato instances.

## Language

### Instances and handoff

**Main instance**:
A Legato a person works from, which owns its tasks. A person may have several, such as a workstation and a laptop.
_Avoid_: home instance, local instance, source

**Worker instance**:
A Legato that works on tasks handed to it by main instances. One worker may serve several main instances.
_Avoid_: remote, VM, cloud instance, delegate

**Instance ID**:
The identity of one Legato, made once and never changed. Other instances know it by this, not by its name.
_Avoid_: instance name, server name

**Handoff**:
A main instance sending one of its tasks to a worker instance to be worked on there.
_Avoid_: delegation, push, offload

**Make-a-change handoff**:
A handoff whose agent changes the code and ends in pull requests.
_Avoid_: fix handoff

**Find-out handoff**:
A handoff whose agent answers a question in notes and pushes nothing.
_Avoid_: investigation, read-only handoff

**Away**:
The state of a task on its main instance while a worker instance holds it.
_Avoid_: delegated, remote

**Recall**:
A main instance taking an away task back before the worker has finished with it.

**Return**:
An away task coming back to its main instance with what the worker did.

**Take back**:
A main instance ending an away task by itself, without the worker's results, when the worker cannot be reached or no longer has the task.
_Avoid_: force recall, abandon

### Agents on a task

**Agent slot**:
A place on a task for one agent at a time, with its own terminal session, execution and saved conversation. A task has a main slot and may have named slots.
_Avoid_: agent seat, second agent

**Main slot**:
The slot of a task's own agent: the handoff's author, and any session the person starts. Anything that addresses "the task's agent" without naming a slot means it.
_Avoid_: primary agent, default agent

**Named slot**:
A slot other than the main one, such as `reviewer`. Only Legato starts agents in one; the person can attach to or stop them.
_Avoid_: extra slot, side agent

### Engrams

**Engram**:
One named individual kept by the engram CLI, with its own memories, open threads and handoffs, that an agent session can be started as. The CLI itself is "the engram CLI".
_Avoid_: identity, persona, profile

### Neighbouring terms

**Delegation**:
A request from Argus that a Legato fix or find out something about a case. Not a handoff.

**Remote ticket**:
A task that comes from a ticket provider such as Jira or Burnlite. Not a task on a worker instance.
