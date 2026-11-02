import { Field } from "@base-ui/react/field";

/** Email settings as typed: the port stays text until save, the password is write-only. */
export interface EmailDraft {
  to: string;
  from: string;
  host: string;
  port: string;
  password: string;
}

export const portValid = (port: string) => /^\d{1,5}$/.test(port) && Number(port) >= 1 && Number(port) <= 65535;

interface Props {
  value: EmailDraft;
  onChange: (value: EmailDraft) => void;
  passwordSaved: boolean;
}

export function EmailFields({ value, onChange, passwordSaved }: Props) {
  const set = (key: keyof EmailDraft) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...value, [key]: e.target.value });

  return (
    <fieldset className="fieldset">
      <legend className="field-label">Email alerts (optional)</legend>
      <Field.Root className="field">
        <Field.Label className="field-label">Send alerts to</Field.Label>
        <Field.Control className="input" type="email" autoComplete="email" value={value.to} onChange={set("to")} />
      </Field.Root>

      {value.to.trim() !== "" && (
        <>
          <p className="field-description">
            Alerts are sent from your own email account. For Gmail, turn on 2-Step Verification and create an app
            password at{" "}
            <a href="https://myaccount.google.com/apppasswords" target="_blank" rel="noreferrer">
              myaccount.google.com/apppasswords
            </a>
            .
          </p>
          <Field.Root className="field">
            <Field.Label className="field-label">Send from (your email address)</Field.Label>
            <Field.Control className="input" type="email" autoComplete="off" value={value.from} onChange={set("from")} />
          </Field.Root>
          <Field.Root className="field">
            <Field.Label className="field-label">App password</Field.Label>
            <Field.Control
              className="input"
              type="password"
              autoComplete="new-password"
              placeholder={passwordSaved ? "Saved. Leave empty to keep it." : ""}
              value={value.password}
              onChange={set("password")}
            />
            <Field.Description className="field-description">
              Stored encrypted on this computer and never shown again.
            </Field.Description>
          </Field.Root>
          <div className="field-row field-row-server">
            <Field.Root className="field">
              <Field.Label className="field-label">SMTP server</Field.Label>
              <Field.Control className="input" spellCheck={false} value={value.host} onChange={set("host")} />
            </Field.Root>
            <Field.Root className="field" invalid={!portValid(value.port)}>
              <Field.Label className="field-label">Port</Field.Label>
              <Field.Control className="input" inputMode="numeric" value={value.port} onChange={set("port")} />
            </Field.Root>
          </div>
        </>
      )}
    </fieldset>
  );
}
