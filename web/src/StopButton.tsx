import { useState } from "react";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { Button } from "@base-ui/react/button";
import { api } from "./api";

export function StopButton({ onStopped }: { onStopped: () => void }) {
  const [error, setError] = useState("");
  const [stopping, setStopping] = useState(false);

  const stop = async () => {
    setStopping(true);
    try {
      await api.quit();
      onStopped();
    } catch (e) {
      setError((e as Error).message);
      setStopping(false);
    }
  };

  return (
    <AlertDialog.Root onOpenChange={() => setError("")}>
      <AlertDialog.Trigger className="button button-ghost">Stop</AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="dialog-backdrop" />
        <AlertDialog.Popup className="card dialog">
          <AlertDialog.Title className="dialog-title">Stop Tonttivahti?</AlertDialog.Title>
          <AlertDialog.Description className="muted">
            No plots are checked and no alerts are sent until you open Tonttivahti again. If it was installed, it also
            starts automatically the next time you log in to Windows.
          </AlertDialog.Description>
          {error && (
            <p className="status status-error" role="alert">
              {error}
            </p>
          )}
          <div className="actions dialog-actions">
            <AlertDialog.Close className="button button-ghost">Keep running</AlertDialog.Close>
            <Button className="button button-danger" disabled={stopping} onClick={stop}>
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
