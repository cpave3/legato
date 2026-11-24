package workflow

// LearningStepTypes are the step types of Legato Learn: a task's agent asked
// to reflect, the bounded packet a curator reads, and the deterministic
// ingestion of the curator's operations.
var LearningStepTypes = []string{"agent.interview", "learning.packet.prepare", "learning.proposals.submit"}

// UsesLearning says whether the definition has a Learn step. A run of such a
// definition is a Learn run: its own task is marked so that it never starts
// another (task.metadata.legato.learning_workflow).
func (d *Definition) UsesLearning() bool {
	for _, step := range d.Steps {
		for _, kind := range LearningStepTypes {
			if step.Type == kind {
				return true
			}
		}
	}
	return false
}
