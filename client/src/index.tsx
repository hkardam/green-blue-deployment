import { render } from "solid-js/web";
import { createSignal, For, onCleanup } from "solid-js";

const API = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
const INTERVAL_MS = 500;
const WINDOW = 120; // points shown on the graph

type Log = { t: number; op: string; status: number | "DOWN"; ms: number; by?: string };
type Point = { avg: number; up: boolean };

const known: number[] = []; // ids we've created, for get/update/delete
// two shades of green so you can tell the servers apart
const COLORS: Record<string, string> = { "green-1": "#4ade80", "green-2": "#a3e635" };
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
  const [stats, setStats] = createSignal({ total: 0, count: 0 });
  const [serving, setServing] = createSignal("");

  const tick = async () => {
    const start = performance.now();
    let entry: Log;
    try {
      const { op, res } = await randomOp();
      const color = res.headers.get("X-Served-By");
      const version = res.headers.get("X-Version");
      const by = color ? `${color} ${version ?? ""}`.trim() : undefined;
      entry = { t: Date.now(), op, status: res.status, ms: performance.now() - start, by };
      setUp(res.status < 500);
      if (by) setServing(by);
    } catch {
      entry = { t: Date.now(), op: "request failed", status: "DOWN", ms: performance.now() - start };
      setUp(false);
    }
    console.log(entry);
    if (entry.status !== "DOWN") setStats((s) => ({ total: s.total + entry.ms, count: s.count + 1 }));
    const { total, count } = stats();
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
        <span style={{ color: "#60a5fa" }}>blue client</span> - API is{" "}
        <span style={{ color: up() ? "#4ade80" : "#f87171" }}>{up() ? "UP" : "DOWN"}</span>
      </h2>
      <div>
        serving:{" "}
        <b style={{ color: COLORS[serving().split(" ")[0]] ?? "#ddd" }}>{serving() || "-"}</b>
      </div>
      <div>
        avg response: {(stats().count ? stats().total / stats().count : 0).toFixed(1)} ms over {stats().count} ok requests
      </div>

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
            {new Date(l.t).toLocaleTimeString()} {String(l.status).padEnd(4)} {l.ms.toFixed(1).padStart(7)}ms {l.op}{" "}
            <span style={{ color: COLORS[(l.by ?? "").split(" ")[0]] ?? "#888" }}>{l.by ? `[${l.by}]` : ""}</span>
          </div>
        )}
      </For>
    </div>
  );
}

render(() => <App />, document.getElementById("root")!);
