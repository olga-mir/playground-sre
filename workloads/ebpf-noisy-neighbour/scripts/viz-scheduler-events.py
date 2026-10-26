#!/usr/bin/env python3
"""
Visualize scheduler events collected by bpftrace
Generates an HTML timeline showing task switches and wakeup latencies
"""

import json
import sys
from pathlib import Path
from collections import defaultdict

def load_events(jsonl_file):
    """Load JSONL events from file"""
    events = []
    with open(jsonl_file) as f:
        for line in f:
            if line.strip():
                try:
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return events

def analyze_events(events):
    """Extract scheduling timeline"""
    timeline = []
    task_colors = {}
    color_idx = 0
    colors = [
        "#FF6B6B", "#4ECDC4", "#45B7D1", "#FFA07A", "#98D8C8",
        "#F7DC6F", "#BB8FCE", "#85C1E2", "#F8B88B", "#ABEBC6"
    ]

    for event in events:
        if event.get("event") == "switch":
            ts_ms = event.get("ts_ms", 0)
            pid = event.get("pid")
            comm = event.get("comm", "?")
            wake_latency = event.get("wake_latency_us", 0)

            if pid not in task_colors:
                task_colors[pid] = colors[color_idx % len(colors)]
                color_idx += 1

            timeline.append({
                "type": "switch",
                "ts_ms": ts_ms,
                "pid": pid,
                "comm": comm,
                "color": task_colors[pid],
                "wake_latency_us": wake_latency,
                "wake_latency_text": f"{wake_latency:.2f}µs" if wake_latency > 0 else "-"
            })

    return timeline, task_colors

def generate_html(timeline, jsonl_file):
    """Generate interactive HTML timeline visualization"""

    # Group by task
    tasks_by_pid = defaultdict(list)
    for event in timeline:
        tasks_by_pid[event["pid"]].append(event)

    # Calculate layout
    max_ts = max((e["ts_ms"] for e in timeline), default=1.0)
    row_height = 60
    rows = sorted(tasks_by_pid.keys())
    num_rows = len(rows)

    # Build HTML
    html = f"""
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Scheduler Events Timeline</title>
    <style>
        body {{
            font-family: monospace;
            margin: 0;
            padding: 20px;
            background: #1e1e1e;
            color: #e0e0e0;
        }}
        .header {{
            margin-bottom: 20px;
        }}
        .header h1 {{
            margin: 0 0 10px 0;
            color: #4ECDC4;
        }}
        .header p {{
            margin: 0;
            font-size: 12px;
            color: #888;
        }}
        svg {{
            border: 1px solid #333;
            background: #0d0d0d;
        }}
        .task-label {{
            font-size: 12px;
            alignment-baseline: middle;
        }}
        .switch-block {{
            cursor: pointer;
            transition: opacity 0.2s;
        }}
        .switch-block:hover {{
            opacity: 0.8;
        }}
        .tooltip {{
            position: absolute;
            background: #2a2a2a;
            border: 1px solid #4ECDC4;
            padding: 8px;
            font-size: 11px;
            border-radius: 4px;
            pointer-events: none;
            z-index: 1000;
            color: #e0e0e0;
        }}
        .info {{
            margin-top: 20px;
            font-size: 12px;
            color: #888;
        }}
    </style>
</head>
<body>
    <div class="header">
        <h1>Scheduler Events Timeline</h1>
        <p>CPU scheduling switches and task wake-up latencies over 2 seconds</p>
        <p>Source: {jsonl_file}</p>
    </div>

    <svg id="timeline" width="1400" height="{num_rows * row_height + 100}"></svg>

    <div class="info">
        <p><strong>Total events:</strong> {len(timeline)}</p>
        <p><strong>Unique tasks:</strong> {num_rows}</p>
        <p><strong>Duration:</strong> {max_ts:.2f}ms</p>
        <p><strong>Color:</strong> Each task has a unique color</p>
        <p><strong>Hover:</strong> Over blocks to see details including wake-up latency</p>
    </div>

    <script>
        const timeline = {json.dumps(timeline)};
        const tasks = {json.dumps(rows)};
        const maxTs = {max_ts};
        const rowHeight = {row_height};
        const margin = {{top: 40, left: 200, right: 20, bottom: 20}};
        const svgWidth = 1400;
        const chartWidth = svgWidth - margin.left - margin.right;

        const svg = document.getElementById('timeline');

        // Draw time axis
        const axisY = margin.top;
        const axisGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g');

        // Background
        const bg = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
        bg.setAttribute('width', svgWidth);
        bg.setAttribute('height', svg.getAttribute('height'));
        bg.setAttribute('fill', '#0d0d0d');
        svg.appendChild(bg);

        // Time grid and labels
        for (let i = 0; i <= 10; i++) {{
            const t = (i / 10) * maxTs;
            const x = margin.left + (t / maxTs) * chartWidth;

            const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
            line.setAttribute('x1', x);
            line.setAttribute('y1', axisY);
            line.setAttribute('x2', x);
            line.setAttribute('y2', svg.getAttribute('height'));
            line.setAttribute('stroke', '#333');
            line.setAttribute('stroke-dasharray', '2,2');
            svg.appendChild(line);

            const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
            label.setAttribute('x', x);
            label.setAttribute('y', axisY - 10);
            label.setAttribute('text-anchor', 'middle');
            label.setAttribute('font-size', '12');
            label.setAttribute('fill', '#888');
            label.textContent = t.toFixed(1) + 'ms';
            svg.appendChild(label);
        }}

        // Draw events
        timeline.forEach(event => {{
            const pidIndex = tasks.indexOf(event.pid);
            const y = margin.top + pidIndex * rowHeight + rowHeight / 2;
            const x = margin.left + (event.ts_ms / maxTs) * chartWidth;
            const width = 8;

            const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
            rect.setAttribute('x', x - width/2);
            rect.setAttribute('y', y - 8);
            rect.setAttribute('width', width);
            rect.setAttribute('height', 16);
            rect.setAttribute('fill', event.color);
            rect.setAttribute('class', 'switch-block');
            rect.setAttribute('opacity', '0.9');

            rect.addEventListener('mouseover', (e) => {{
                rect.setAttribute('opacity', '1');
                const tooltip = document.createElement('div');
                tooltip.className = 'tooltip';
                tooltip.style.left = e.pageX + 10 + 'px';
                tooltip.style.top = e.pageY + 10 + 'px';
                tooltip.innerHTML = `
                    <b>${{event.comm}}</b> (PID ${{event.pid}})<br/>
                    Time: ${{event.ts_ms.toFixed(2)}}ms<br/>
                    Wake latency: ${{event.wake_latency_text}}
                `;
                document.body.appendChild(tooltip);

                rect.addEventListener('mouseout', () => {{
                    rect.setAttribute('opacity', '0.9');
                    tooltip.remove();
                }}, {{once: true}});
            }});

            svg.appendChild(rect);
        }});

        // Draw task labels
        tasks.forEach((pid, i) => {{
            const y = margin.top + i * rowHeight + rowHeight / 2;
            const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
            label.setAttribute('x', 10);
            label.setAttribute('y', y + 4);
            label.setAttribute('font-size', '12');
            label.setAttribute('fill', '#888');
            label.setAttribute('class', 'task-label');
            label.textContent = `PID ${{pid}}`;
            svg.appendChild(label);
        }});
    </script>
</body>
</html>
"""
    return html

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 viz-scheduler-events.py <events.jsonl> [output.html]")
        sys.exit(1)

    jsonl_file = sys.argv[1]
    output_file = sys.argv[2] if len(sys.argv) > 2 else "scheduler-timeline.html"

    if not Path(jsonl_file).exists():
        print(f"Error: {jsonl_file} not found")
        sys.exit(1)

    print(f"Loading events from {jsonl_file}...")
    events = load_events(jsonl_file)
    print(f"Loaded {len(events)} events")

    print("Analyzing timeline...")
    timeline, task_colors = analyze_events(events)
    print(f"Found {len(set(e['pid'] for e in timeline))} unique tasks")

    print(f"Generating HTML visualization...")
    html = generate_html(timeline, jsonl_file)

    with open(output_file, 'w') as f:
        f.write(html)

    print(f"✓ Visualization saved to: {output_file}")
    print(f"  Open in a browser to see the timeline")

if __name__ == "__main__":
    main()
