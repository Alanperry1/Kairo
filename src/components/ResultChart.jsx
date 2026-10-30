import React from 'react';

export default function ResultChart({ result, adapter }) {
  const series = result.series || [];
  const points = series.flatMap((s) => s.points);
  if (!points.length) return null;
  let minX = 0; let maxX = 1; let minY = 0; let maxY = 1;
  for (const point of points) { minX = Math.min(minX, point.x); maxX = Math.max(maxX, point.x); minY = Math.min(minY, point.y); maxY = Math.max(maxY, point.y); }
  if (maxX === minX) maxX++;
  if (maxY === minY) maxY++;
  const x = (v) => 62 + (v - minX) / (maxX - minX) * 618;
  const y = (v) => 220 - (v - minY) / (maxY - minY) * 186;
  const colors = ['#b9a6f5', '#7fb8de', '#91c5a4', '#d9b37e'];
  const defaultAxes = adapter === 'cs.sorting' ? ['Input size', 'Median runtime (ms)'] : adapter === 'statistics.welch' ? ['Group (0 = A, 1 = B)', 'Mean value'] : adapter === 'physics.projectile' ? ['Horizontal distance (m)', 'Height (m)'] : ['x', 'y'];
  const axes = [result.x_axis_label || defaultAxes[0], result.y_axis_label || defaultAxes[1]];
  return <div className="result-chart"><svg viewBox="0 0 720 275" role="img" aria-label={`Computed results: ${axes[1]} by ${axes[0]}`}>
    {[0, 1, 2, 3, 4].map((i) => <g key={i}><line x1="62" x2="680" y1={34 + i * 46.5} y2={34 + i * 46.5} stroke="#302c37"/><text x="7" y={38 + i * 46.5}>{(maxY - (maxY - minY) * i / 4).toPrecision(3)}</text><text x={62 + i * 154.5} y="240">{(minX + (maxX - minX) * i / 4).toPrecision(3)}</text></g>)}
    {series.map((s, i) => <g key={s.name}><polyline fill="none" stroke={colors[i % colors.length]} strokeWidth="2" points={s.points.map((p) => `${x(p.x)},${y(p.y)}`).join(' ')}/>{s.points.length < 10 && s.points.map((p, j) => <circle key={j} cx={x(p.x)} cy={y(p.y)} r="3" fill={colors[i % colors.length]}/>)}</g>)}
    <text x="62" y="18">{axes[1]}</text><text x="270" y="265">{axes[0]}</text>
  </svg><div className="result-legend">{series.map((s, i) => <span key={s.name}><i style={{ background: colors[i % colors.length] }}/>{s.name}</span>)}</div></div>;
}
