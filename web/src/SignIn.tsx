import { useState } from "react";
import { Button } from "@base-ui/react/button";
import { Field } from "@base-ui/react/field";
import { USERNAME_PATTERN } from "./api";

export function SignIn({ onSignIn }: { onSignIn: (username: string) => void }) {
  const [name, setName] = useState("");
  const username = name.trim().toLowerCase();
  const valid = USERNAME_PATTERN.test(username);

  return (
    <main className="sign-in">
      <form
        className="card sign-in-card"
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) onSignIn(username);
        }}
      >
        <p className="brand">Tonttivahti</p>
        <h1 className="sign-in-title">Know the moment Oulu lists the plot you want.</h1>
        <Field.Root className="field" invalid={name !== "" && !valid}>
          <Field.Label className="field-label">Username</Field.Label>
          <Field.Control
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoComplete="username"
            autoFocus
          />
          <Field.Description className="field-description">
            {name !== "" && !valid
              ? "Use 1–32 letters, digits, dots, dashes or underscores."
              : "No password. The name only keeps your alerts apart from others on this computer."}
          </Field.Description>
        </Field.Root>
        <Button type="submit" className="button button-primary" disabled={!valid}>
          Continue
        </Button>
      </form>
    </main>
  );
}
