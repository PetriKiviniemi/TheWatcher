import { useEffect, useRef, useState } from "react";
import { Button } from "@base-ui/react/button";
import { api, siteUrl, type Profile, type UnitClasses } from "./api";
import { SettingsForm } from "./SettingsForm";
import { StopButton } from "./StopButton";

const REFRESH_MS = 60_000;
const number = new Intl.NumberFormat("fi-FI", { maximumFractionDigits: 0 });
const time = new Intl.DateTimeFormat("en-GB", { weekday: "short", hour: "2-digit", minute: "2-digit" });

const neverChecked = (p: Profile) => new Date(p.lastCheck).getFullYear() < 2000; // Go's zero time

interface Props {
  username: string;
  onSignOut: () => void;
  onStopped: () => void;
}

export function Dashboard({ username, onSignOut, onStopped }: Props) {
  const [profile, setProfile] = useState<Profile | null>(null);
  const [unitClasses, setUnitClasses] = useState<UnitClasses>({});
  const [loadError, setLoadError] = useState("");
  const savedAt = useRef(0); // a poll that started before the last save must not overwrite it

  useEffect(() => {
    let active = true;
    let typesLoaded = false;
    const load = async () => {
      const startedAt = Date.now();
      try {
        const latest = await api.profile(username);
        if (active && startedAt >= savedAt.current) {
          setProfile(latest);
          setLoadError("");
        }
        if (!typesLoaded) {
          const classes = await api.unitClasses(); // retried on every poll until it succeeds
          typesLoaded = true;
          if (active) setUnitClasses(classes);
        }
      } catch (e) {
        if (active) setLoadError((e as Error).message);
      }
    };
    load();
    const timer = setInterval(load, REFRESH_MS); // the watcher keeps checking while this page is open
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [username]);

  const onSaved = (saved: Profile) => {
    savedAt.current = Date.now();
    setProfile(saved);
  };

  return (
    <div className="shell">
      <header className="topbar">
        <p className="brand">Tonttivahti</p>
        <div className="topbar-user">
          <span className="user-chip">{username}</span>
          <Button className="button button-ghost" onClick={onSignOut}>
            Switch user
          </Button>
          <StopButton onStopped={onStopped} />
        </div>
      </header>

      <main className="layout">
        <div className="page-heading">
          <h1>Plot watch</h1>
          <p className="muted">Building plots from the City of Oulu, checked automatically while this app runs.</p>
        </div>

        {(loadError || profile?.lastError) && (
          <p className="banner" role="alert">
            {loadError || `Last check failed: ${profile?.lastError}`}
          </p>
        )}

        {profile && (
          <>
            <Stats profile={profile} />
            <SettingsForm username={username} initial={profile} unitClasses={unitClasses} onSaved={onSaved} />
            <Matches profile={profile} unitClasses={unitClasses} />
          </>
        )}
      </main>
    </div>
  );
}

function Stats({ profile }: { profile: Profile }) {
  return (
    <section className="stats" aria-label="Summary">
      <div className="card stat stat-signal">
        <p className="stat-label">Matching plots</p>
        <p className="stat-value">{neverChecked(profile) ? "–" : profile.matches.length}</p>
      </div>
      <div className="card stat">
        <p className="stat-label">Last check</p>
        <p className="stat-value">{neverChecked(profile) ? "Not yet" : time.format(new Date(profile.lastCheck))}</p>
        <p className="stat-note">Every {profile.intervalMinutes} min</p>
      </div>
      <div className="card stat">
        <p className="stat-label">Phone alerts</p>
        <p className="stat-value">{profile.topic ? "On" : "Off"}</p>
        <p className="stat-note">{profile.topic ? "Sent with ntfy" : "Add an ntfy topic below"}</p>
      </div>
    </section>
  );
}

const withUnit = (value: number | null, unit: string) => (value === null ? "–" : `${number.format(value)} ${unit}`);

function Matches({ profile, unitClasses }: { profile: Profile; unitClasses: UnitClasses }) {
  return (
    <section className="card matches" aria-labelledby="matches-title">
      <div className="card-heading">
        <h2 id="matches-title">Matching plots</h2>
        <p className="muted">New plots that match trigger an alert. These are the ones listed right now.</p>
      </div>
      {profile.matches.length === 0 ? (
        <p className="empty">
          {neverChecked(profile)
            ? "Save your filters to see which plots match."
            : "Nothing matches right now. You’ll get an alert as soon as something does."}
        </p>
      ) : (
        <div className="table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">Address</th>
                <th scope="col">Type</th>
                <th scope="col" className="numeric">Size</th>
                <th scope="col" className="numeric">Price</th>
                <th scope="col" className="numeric">Rent</th>
              </tr>
            </thead>
            <tbody>
              {profile.matches.map((site) => (
                <tr key={site.Id}>
                  <td>
                    <a href={siteUrl(site)} target="_blank" rel="noreferrer" className="plot-link">
                      {site.Address}
                    </a>
                    <span className="muted plot-area">{site.SubArea}</span>
                  </td>
                  <td>{unitClasses[site.UnitClassId]?.Name ?? "–"}</td>
                  <td className="numeric">{withUnit(site.Area, "m²")}</td>
                  <td className="numeric">{withUnit(site.Price, "€")}</td>
                  <td className="numeric">{withUnit(site.Rent, "€/yr")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
