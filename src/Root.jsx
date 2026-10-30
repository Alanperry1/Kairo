import React, { lazy, Suspense } from 'react';
import HomePage from './HomePage';

const Dashboard = lazy(() => import('./App'));
export default function Root() {
  if (window.location.pathname === '/app' || window.location.pathname.startsWith('/app/')) {
    return <Suspense fallback={<div className="loading-state">Opening your workspace…</div>}><Dashboard/></Suspense>;
  }
  return <HomePage/>;
}
