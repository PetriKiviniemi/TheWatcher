import { useState } from "react";
import { Button } from "@base-ui/react/button";
import { Checkbox } from "@base-ui/react/checkbox";
import { CheckboxGroup } from "@base-ui/react/checkbox-group";
import { Field } from "@base-ui/react/field";
import { api, MIN_INTERVAL_MINUTES, type Profile, type Settings, type UnitClasses } from "./api";

interface Props {
  username: string;
  initial: Profile;
  unitClasses: UnitClasses;
  onSaved: (profile: Profile) => void;
}

// Inputs hold text so half-typed values survive until save.
const toText = (n: number | null) => (n === null ? "" : String(n));

/** Parses "80 000" or "80000,5"; empty means no limit, undefined means invalid. */
function parseLimit(text: string): number | null | undefined {
  const cleaned = text.replace(/[\s\u00a0]/g, "").replace(",", ".");
  if (cleaned === "") return null;
  const n = Number(cleaned);
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

const newTopic = () => `tonttivahti-${crypto.randomUUID().replaceAll("-", "")}`;

export function SettingsForm({ username, initial, unitClasses, onSaved }: Props) {
  const [types, setTypes] = useState(initial.filter.unitClassIds.map(String));
  const [keywords, setKeywords] = useState(initial.filter.keywords.join(", "));
  const [maxPrice, setMaxPrice] = useState(toText(initial.filter.maxPrice));
  const [maxRent, setMaxRent] = useState(toText(initial.filter.maxRent));
  const [minArea, setMinArea] = useState(toText(initial.filter.minArea));
  const [topic, setTopic] = useState(initial.topic);
  const [intervalText, setIntervalText] = useState(String(initial.intervalMinutes));
  const [savedTopic, setSavedTopic] = useState(initial.topic);
  const [status, setStatus] = useState<{ text: string; error?: boolean }>({ text: "" });
  const [busy, setBusy] = useState(false);

  const intervalValid = Number.isInteger(Number(intervalText)) && Number(intervalText) >= MIN_INTERVAL_MINUTES;
  const limits = [maxPrice, maxRent, minArea].map(parseLimit);
  const formValid = intervalValid && !limits.includes(undefined);
  const sortedClasses = Object.entries(unitClasses)
    .filter(([id]) => id !== "0") // "Tuntematon" (unknown) is not a real choice
    .sort(([, a], [, b]) => a.Order - b.Order);

  async function run(action: () => Promise<string>) {
    setBusy(true);
    setStatus({ text: "" });
    try {
      setStatus({ text: await action() });
    } catch (e) {
      setStatus({ text: (e as Error).message, error: true });
    } finally {
      setBusy(false);
    }
  }

  const save = () =>
    run(async () => {
      const settings: Settings = {
        topic: topic.trim(),
        intervalMinutes: Number(intervalText),
        filter: {
          unitClassIds: types.map(Number),
          keywords: keywords.split(","),
          maxPrice: limits[0] ?? null,
          maxRent: limits[1] ?? null,
          minArea: limits[2] ?? null,
        },
      };
      const saved = await api.save(username, settings);
      setSavedTopic(saved.topic);
      onSaved(saved);
      return "Saved. Matching plots are up to date.";
    });

  const sendTest = () => run(async () => (await api.testAlert(username), "Test alert sent. Check your phone."));

  return (
    <form
      className="card settings"
      aria-labelledby="settings-title"
      onSubmit={(e) => {
        e.preventDefault();
        if (formValid) save();
      }}
    >
      <div className="card-heading">
        <h2 id="settings-title">Filters and alerts</h2>
      </div>

      <fieldset className="fieldset">
        <legend className="field-label">Plot types</legend>
        <CheckboxGroup className="chips" value={types} onValueChange={setTypes}>
          {sortedClasses.map(([id, unitClass]) => (
            <label key={id} className="chip">
              <Checkbox.Root value={id} className="chip-box">
                <Checkbox.Indicator className="chip-check">
                  <svg viewBox="0 0 12 12" aria-hidden="true">
                    <path d="M2.5 6.5 5 9l4.5-6" />
                  </svg>
                </Checkbox.Indicator>
              </Checkbox.Root>
              {unitClass.Name}
            </label>
          ))}
        </CheckboxGroup>
        <p className="field-description">None selected means every type.</p>
      </fieldset>

      <Field.Root className="field">
        <Field.Label className="field-label">Keywords</Field.Label>
        <Field.Control className="input" value={keywords} onChange={(e) => setKeywords(e.target.value)} />
        <Field.Description className="field-description">
          Comma-separated. Matches address, area or property ID, e.g. “Haukipudas, Ritaharju”.
        </Field.Description>
      </Field.Root>

      <div className="field-row">
        <NumberInput label="Max price" unit="€" value={maxPrice} onChange={setMaxPrice} />
        <NumberInput label="Max rent" unit="€/yr" value={maxRent} onChange={setMaxRent} />
        <NumberInput label="Min size" unit="m²" value={minArea} onChange={setMinArea} />
      </div>
      <p className="field-description">
        {limits.includes(undefined)
          ? "Limits must be positive numbers, e.g. 80 000."
          : "Leave empty for any. A price limit hides rent-only plots, and the other way round."}
      </p>

      <hr className="divider" />

      <Field.Root className="field">
        <Field.Label className="field-label">ntfy topic</Field.Label>
        <div className="input-with-action">
          <Field.Control
            className="input"
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="tonttivahti-…"
            spellCheck={false}
          />
          <Button type="button" className="button button-ghost" onClick={() => setTopic(newTopic())}>
            Generate
          </Button>
        </div>
        <Field.Description className="field-description">
          Install the ntfy app on your phone and subscribe to this topic. Anyone who knows the name can read it, so keep
          the generated one.
        </Field.Description>
      </Field.Root>

      <Field.Root className="field field-narrow" invalid={!intervalValid}>
        <Field.Label className="field-label">Check every (minutes)</Field.Label>
        <Field.Control
          className="input"
          type="number"
          inputMode="numeric"
          min={MIN_INTERVAL_MINUTES}
          step={1}
          value={intervalText}
          onChange={(e) => setIntervalText(e.target.value)}
        />
        <Field.Description className="field-description">
          {intervalValid ? `At least ${MIN_INTERVAL_MINUTES}, to be fair to the city’s service.` : `Use a whole number of ${MIN_INTERVAL_MINUTES} or more.`}
        </Field.Description>
      </Field.Root>

      <div className="actions">
        <Button type="submit" className="button button-primary" disabled={busy || !formValid}>
          {busy ? "Saving…" : "Save"}
        </Button>
        <Button
          type="button"
          className="button button-ghost"
          disabled={busy || !savedTopic || topic.trim() !== savedTopic}
          onClick={sendTest}
        >
          Send test alert
        </Button>
      </div>
      <p className={status.error ? "status status-error" : "status"} role="status">
        {status.text}
      </p>
    </form>
  );
}

function NumberInput(props: { label: string; unit: string; value: string; onChange: (v: string) => void }) {
  return (
    <Field.Root className="field" invalid={parseLimit(props.value) === undefined}>
      <Field.Label className="field-label">
        {props.label} <span className="muted">({props.unit})</span>
      </Field.Label>
      <Field.Control
        className="input"
        inputMode="decimal"
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
      />
    </Field.Root>
  );
}
