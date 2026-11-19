// A small app the Scope source tests serve with Vite, under React 18 and
// React 19. The tests look for the lines marked "pin:" below: keep each
// marked element on its own line.

function SaveButton({ label }) {
  return (
    <button type="button" className="save">
      {label}
    </button>
  )
}

const Toolbar = ({ children }) => (
  <div className="toolbar">
    {children}
  </div>
)

const Badge = () => <span className="badge">new</span>

function App() {
  return (
    <main>
      <h1 data-testid="title">Settings</h1>
      <Toolbar>
        <SaveButton label="Save" />
        <Badge />
      </Toolbar>
    </main>
  )
}

export default App
