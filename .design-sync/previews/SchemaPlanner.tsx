import { SchemaPlanner } from 'cloudpam-ui'

// SchemaPlanner is the wizard shell: it owns the step state internally and
// takes no props, so a card shows it at step 1 (template selection) with the
// step rail across the top.
export function Wizard() {
  return (
    <div style={{ height: 900 }}>
      <SchemaPlanner />
    </div>
  )
}
