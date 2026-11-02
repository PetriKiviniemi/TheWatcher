import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import { Dashboard } from "./Dashboard";
import { SignIn } from "./SignIn";
import "./tokens.css";
import "./app.css";

const USERNAME_KEY = "tonttivahti.username";

// Mock account: the username only tells profiles apart, so remembering it locally is enough.
function App() {
  const [username, setUsername] = useState(() => localStorage.getItem(USERNAME_KEY));
  const [stopped, setStopped] = useState(false);

  const signIn = (name: string) => {
    localStorage.setItem(USERNAME_KEY, name);
    setUsername(name);
  };
  const signOut = () => {
    localStorage.removeItem(USERNAME_KEY);
    setUsername(null);
  };

  if (stopped) {
    return (
      <main className="sign-in">
        <div className="card sign-in-card">
          <p className="brand">Tonttivahti</p>
          <h1 className="sign-in-title">Tonttivahti is stopped.</h1>
          <p className="muted">Open Tonttivahti again to start watching. You can close this tab.</p>
        </div>
      </main>
    );
  }
  return username ? (
    <Dashboard username={username} onSignOut={signOut} onStopped={() => setStopped(true)} />
  ) : (
    <SignIn onSignIn={signIn} />
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
