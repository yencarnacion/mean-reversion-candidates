const state = {
  mode: "live",
  timer: null,
  playing: false,
  playTimer: null,
  livePinned: false,
  liveCursor: null,
  lastLiveAsOf: null,
};

const $ = (id) => document.getElementById(id);

function fmtDateTime(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString([], {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function fmtClock(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString([], {
    weekday: "short",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZoneName: "short",
  });
}

function fmtMoney(value) {
  if (!Number.isFinite(value)) return "-";
  if (value >= 1_000_000_000) return `$${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(1)}K`;
  return `$${value.toFixed(0)}`;
}

function signed(value, suffix = "") {
  if (!Number.isFinite(value)) return "-";
  const prefix = value > 0 ? "+" : "";
  return `${prefix}${value.toFixed(2)}${suffix}`;
}

async function loadConfig() {
  const res = await fetch("/api/config");
  const data = await res.json();
  renderFormula(data.formula);
  $("symbolCount").textContent = data.symbols.length;

  const now = new Date();
  $("histDate").value = now.toISOString().slice(0, 10);
  $("histTime").value = "20:00";
}

function renderFormula(formula) {
  $("formulaSummary").textContent = formula.summary;
  $("formulaGrid").innerHTML = Object.entries(formula.components)
    .map(([key, value]) => `<div class="formula-item"><strong>${key}</strong><span>${value}</span></div>`)
    .join("");
}

async function loadRankings() {
  const pinnedLive = state.mode === "live" && state.livePinned;
  const url = state.mode === "historical" ? historicalUrl() : pinnedLive ? liveCursorUrl() : "/api/rankings";
  const res = await fetch(url);
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "request failed");
  if (pinnedLive) {
    data.mode = "live paused";
  }
  renderSnapshot(data);
}

function historicalUrl() {
  const params = new URLSearchParams({
    mode: "historical",
    date: $("histDate").value,
    time: $("histTime").value,
  });
  return `/api/rankings?${params.toString()}`;
}

function liveCursorUrl() {
  ensureLiveCursor();
  const parts = dateTimeParts(state.liveCursor);
  const params = new URLSearchParams({
    mode: "historical",
    date: parts.date,
    time: parts.time,
  });
  return `/api/rankings?${params.toString()}`;
}

function renderSnapshot(data) {
  $("status").textContent = `${data.mode} - ${data.status}`;
  $("symbolCount").textContent = data.symbols;
  $("asOf").textContent = fmtDateTime(data.as_of);
  $("updatedAt").textContent = fmtDateTime(data.updated_at);
  $("dataClock").textContent = fmtClock(data.as_of);
  $("dataClockMode").textContent = dataClockModeLabel(data.mode);
  if (state.mode === "live" && !state.livePinned && data.as_of) {
    state.lastLiveAsOf = new Date(data.as_of);
    state.liveCursor = new Date(data.as_of);
  }
  updateLiveControls();
  if (data.formula) renderFormula(data.formula);
  renderRows(data.rankings || []);
}

function dataClockModeLabel(apiMode) {
  if (state.mode === "historical") return "Historical replay minute";
  if (state.livePinned) return "Live paused minute";
  if (apiMode === "live paused") return "Live paused minute";
  return "Latest live minute";
}

function renderRows(rows) {
  $("rankings").innerHTML = rows.map((r) => {
    const gradeClass = (r.grade || "").toLowerCase();
    const sideClass = r.side === "Long bounce" ? "side-long" : r.side === "Short fade" ? "side-short" : "";
    return `
      <tr class="stock-main-row">
        <td>${r.rank || "-"}</td>
        <td><a href="${r.chart_url || "#"}" target="_blank" rel="noreferrer">${r.symbol}</a></td>
        <td class="${sideClass}">${r.side}</td>
        <td><span class="score"><span class="grade ${gradeClass}">${r.grade}</span><strong>${Number(r.score || 0).toFixed(1)}</strong></span></td>
        <td>${Number(r.price || 0).toFixed(2)}</td>
        <td>${Number(r.vwap || 0).toFixed(2)}</td>
        <td class="${r.move_from_vwap_pct < 0 ? "negative" : "positive"}">${signed(r.move_from_vwap_pct, "%")}</td>
        <td class="${r.day_move_atr < 0 ? "negative" : "positive"}">${signed(r.day_move_atr, "x")}</td>
        <td class="${r.vwap_stretch_atr < 0 ? "negative" : "positive"}">${signed(r.vwap_stretch_atr, "x")}</td>
        <td>${Number(r.atr_percent || 0).toFixed(2)}%</td>
        <td class="${r.return_30m_pct < 0 ? "negative" : "positive"}">${signed(r.return_30m_pct, "%")}</td>
        <td>${Number(r.z_score_30m || 0).toFixed(2)}</td>
        <td>${Number(r.range_position_pct || 0).toFixed(1)}%</td>
        <td>${fmtMoney(r.dollar_volume || 0)}</td>
      </tr>
      <tr class="stock-why-row">
        <td colspan="14">
          <div class="why-line">
            <span class="why-label">Why</span>
            <span>${r.reason || ""}</span>
            <span class="why-components">${componentTitle(r.components)}</span>
          </div>
        </td>
      </tr>`;
  }).join("");
}

function componentTitle(c = {}) {
  return [
    `VWAP ${c.vwap_extreme || 0}`,
    `30m ${c.statistical_move || 0}`,
    `ATR ${c.daily_atr_move || 0}`,
    `Range ${c.range_extension || 0}`,
    `Liquidity ${c.volume_confirmation || 0}`,
    `Reversal ${c.reversal_evidence || 0}`,
    `Penalty -${c.trend_penalty || 0}`,
  ].join(" | ");
}

function setMode(mode) {
  state.mode = mode;
  $("liveBtn").classList.toggle("active", mode === "live");
  $("histBtn").classList.toggle("active", mode === "historical");
  $("historicalPanel").classList.toggle("hidden", mode !== "historical");
  $("livePlayback").classList.toggle("hidden", mode !== "live");
  updateLiveControls();
  loadRankings().catch(showError);
}

function dateTimeParts(date) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "America/New_York",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(date).reduce((acc, part) => {
    acc[part.type] = part.value;
    return acc;
  }, {});
  return {
    date: `${parts.year}-${parts.month}-${parts.day}`,
    time: `${parts.hour}:${parts.minute}`,
  };
}

function addMinutes(date, minutes) {
  return new Date(date.getTime() + minutes * 60_000);
}

function ensureLiveCursor() {
  if (!state.liveCursor) {
    state.liveCursor = state.lastLiveAsOf ? new Date(state.lastLiveAsOf) : new Date();
  }
}

function setLivePinned(value) {
  state.livePinned = value;
  if (value) ensureLiveCursor();
  updateLiveControls();
}

function stepLive(minutes) {
  ensureLiveCursor();
  const next = addMinutes(state.liveCursor, minutes);
  if (minutes > 0 && state.lastLiveAsOf && next > state.lastLiveAsOf) {
    state.liveCursor = new Date(state.lastLiveAsOf);
  } else {
    state.liveCursor = next;
  }
  setLivePinned(true);
  loadRankings().catch(showError);
}

function pauseLive() {
  ensureLiveCursor();
  setLivePinned(true);
  loadRankings().catch(showError);
}

function goLiveLatest() {
  state.livePinned = false;
  updateLiveControls();
  loadRankings().catch(showError);
}

function updateLiveControls() {
  const isLiveMode = state.mode === "live";
  $("livePlayback").classList.toggle("hidden", !isLiveMode);
  $("pauseLiveBtn").classList.toggle("active", isLiveMode && state.livePinned);
  $("pauseLiveBtn").textContent = state.livePinned ? "Paused" : "Pause";
}

function sliderToTime(value) {
  const minutes = Number(value);
  const hh = Math.floor(minutes / 60);
  const mm = minutes % 60;
  return `${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}`;
}

function timeToSlider(value) {
  const [hh, mm] = value.split(":").map(Number);
  return hh * 60 + mm;
}

function startReplay() {
  if (state.playing) return;
  state.playing = true;
  state.playTimer = setInterval(() => {
    const slider = $("timeSlider");
    const next = Math.min(Number(slider.value) + 1, Number(slider.max));
    slider.value = String(next);
    $("histTime").value = sliderToTime(next);
    loadRankings().catch(showError);
    if (next >= Number(slider.max)) stopReplay();
  }, 1000);
}

function stopReplay() {
  state.playing = false;
  clearInterval(state.playTimer);
  state.playTimer = null;
}

function showError(err) {
  $("status").textContent = err.message || String(err);
}

async function refreshNow() {
  if (state.mode === "live") {
    if (state.livePinned) {
      await loadRankings();
      return;
    }
    const res = await fetch("/api/refresh", { method: "POST" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "refresh failed");
    renderSnapshot(data);
    return;
  }
  await loadRankings();
}

$("liveBtn").addEventListener("click", () => setMode("live"));
$("histBtn").addEventListener("click", () => setMode("historical"));
$("refreshBtn").addEventListener("click", () => refreshNow().catch(showError));
$("back1Btn").addEventListener("click", () => stepLive(-1));
$("pauseLiveBtn").addEventListener("click", pauseLive);
$("forward1Btn").addEventListener("click", () => stepLive(1));
$("latestBtn").addEventListener("click", goLiveLatest);
$("histDate").addEventListener("change", () => loadRankings().catch(showError));
$("histTime").addEventListener("change", () => {
  $("timeSlider").value = String(timeToSlider($("histTime").value));
  loadRankings().catch(showError);
});
$("timeSlider").addEventListener("input", () => {
  $("histTime").value = sliderToTime($("timeSlider").value);
});
$("timeSlider").addEventListener("change", () => loadRankings().catch(showError));
$("playBtn").addEventListener("click", startReplay);
$("stopBtn").addEventListener("click", stopReplay);

loadConfig()
  .then(loadRankings)
  .catch(showError);

state.timer = setInterval(() => {
  if (state.mode === "live" && !state.livePinned) loadRankings().catch(showError);
}, 60_000);
