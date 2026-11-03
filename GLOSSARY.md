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

### Neighbouring terms

**Delegation**:
A request from Argus that a Legato fix or find out something about a case. Not a handoff.

**Remote ticket**:
A task that comes from a ticket provider such as Jira. Not a task on a worker instance.
