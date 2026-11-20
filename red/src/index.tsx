import { render } from "solid-js/web";
import { createSignal, For, onCleanup } from "solid-js";

const API = import.meta.env.VITE_BLACK_URL ?? "http://localhost:8080";
const INTERVAL_MS = 500;
const WINDOW = 120; // points shown on the graph

type Log = { t: number; op: string; status: number | "DOWN"; ms: number };
type Point = { avg: number; up: boolean };

const known: number[] = []; // ids we've created, for get/update/delete
const pick = <T,>(a: T[]) => a[Math.floor(Math.random() * a.length)];

async function randomOp(): Promise<{ op: string; res: Response }> {
  const id = known.length ? pick(known) : 1;
  const ops = [
    async () => {
      const res = await fetch(`${API}/todos`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: `task-${Date.now()}`, done: false }),
      });
      if (res.ok) known.push((await res.clone().json()).id);
      return { op: "POST /todos", res };
    },
    async () => ({ op: "GET /todos", res: await fetch(`${API}/todos`) }),
    async () => ({ op: `GET /todos/${id}`, res: await fetch(`${API}/todos/${id}`) }),
    async () => ({
      op: `PUT /todos/${id}`,
      res: await fetch(`${API}/todos/${id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: `updated-${id}`, done: Math.random() > 0.5 }),
      }),
    }),
    async () => {
      const res = await fetch(`${API}/todos/${id}`, { method: "DELETE" });
      if (res.ok) known.splice(known.indexOf(id), 1);
      return { op: `DELETE /todos/${id}`, res };
    },
  ];
  return pick(ops)();
}

function App() {
  const [logs, setLogs] = createSignal<Log[]>([]);
  const [points, setPoints] = createSignal<Point[]>([]);
  const [up, setUp] = createSignal(true);
  let total = 0, count = 0;

  const tick = async () => {
    const start = performance.now();
    let entry: Log;
    try {
      const { op, res } = await randomOp();
      entry = { t: Date.now(), op, status: res.status, ms: performance.now() - start };
      setUp(res.status < 500);
    } catch {
      entry = { t: Date.now(), op: "request failed", status: "DOWN", ms: performance.now() - start };
      setUp(false);
    }
    console.log(entry);
    if (entry.status !== "DOWN") { total += entry.ms; count++; }
    setLogs((l) => [entry, ...l].slice(0, 50));
    setPoints((p) => [...p, { avg: count ? total / count : 0, up: up() }].slice(-WINDOW));
  };

  const timer = setInterval(tick, INTERVAL_MS);
  onCleanup(() => clearInterval(timer));

  const W = 800, H = 200;
  const max = () => Math.max(1, ...points().map((p) => p.avg)) * 1.2;
  const x = (i: number) => (i / (WINDOW - 1)) * W;
  const y = (v: number) => H - (v / max()) * H;
  const path = () => points().map((p, i) => `${i ? "L" : "M"}${x(i)},${y(p.avg)}`).join("");

  return (
    <div style={{ padding: "16px", "max-width": "840px", margin: "auto" }}>
      <h2>
        black is{" "}
        <span style={{ color: up() ? "#4ade80" : "#f87171" }}>{up() ? "UP" : "DOWN"}</span>
      </h2>
      <div>avg response: {(count ? total / count : 0).toFixed(1)} ms over {count} ok requests</div>

      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: "100%", background: "#1a1a1a", "margin-top": "12px" }}>
        <For each={points()}>
          {(p, i) => !p.up && <rect x={x(i())} y={0} width={W / WINDOW} height={H} fill="#7f1d1d" />}
        </For>
        <path d={path()} fill="none" stroke="#60a5fa" stroke-width="2" />
        <text x="4" y="14" fill="#888" font-size="12">{max().toFixed(0)} ms</text>
      </svg>

      <h3>log</h3>
      <For each={logs()}>
        {(l) => (
          <div style={{ color: l.status === "DOWN" || (l.status as number) >= 500 ? "#f87171" : "#ddd" }}>
            {new Date(l.t).toLocaleTimeString()} {String(l.status).padEnd(4)} {l.ms.toFixed(1).padStart(7)}ms {l.op}
          </div>
        )}
      </For>
    </div>
  );
}

render(() => <App />, document.getElementById("root")!);
