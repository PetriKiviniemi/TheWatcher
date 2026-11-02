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

  const signIn = (name: string) => {
    localStorage.setItem(USERNAME_KEY, name);
    setUsername(name);
  };
  const signOut = () => {
    localStorage.removeItem(USERNAME_KEY);
    setUsername(null);
  };

  return username ? <Dashboard username={username} onSignOut={signOut} /> : <SignIn onSignIn={signIn} />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
