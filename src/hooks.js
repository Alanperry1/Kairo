import { useCallback, useEffect, useRef, useState } from 'react';

export function useDebounced(value, delay = 250) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => { const timer = setTimeout(() => setDebounced(value), delay); return () => clearTimeout(timer); }, [value, delay]);
  return debounced;
}

export function useWorkspace(api, filters, revision) {
  const [state, setState] = useState({
    experiments: [], total: 0, dashboard: null, settings: null, projects: [], datasets: [], tools: [], reports: [],
    loading: true, error: '', unauthorized: false, connected: false,
  });
  const key = JSON.stringify(filters);
  useEffect(() => {
    const controller = new AbortController();
    let timer;
    const signal = controller.signal;
    async function load() {
      const jobs = {
        experiments: () => api.listExperiments(JSON.parse(key), signal),
        dashboard: () => api.dashboard(signal), settings: () => api.settings(signal),
        projects: () => api.projects(signal), datasets: () => api.datasets(signal),
        tools: () => api.tools(signal), reports: () => api.reports(signal),
      };
      const names = Object.keys(jobs);
      const results = await Promise.allSettled(names.map((name) => jobs[name]()));
      if (signal.aborted) return;
      setState((previous) => {
        const next = { ...previous, loading: false, error: '', unauthorized: false };
        let successes = 0;
        results.forEach((result, index) => {
          const name = names[index];
          if (result.status === 'fulfilled') {
            successes++;
            const value = result.value;
            if (name === 'experiments') { next.experiments = value.data; next.total = value.total; }
            else if (name === 'dashboard' || name === 'settings') next[name] = value;
            else next[name] = value.data;
          } else {
            if (result.reason.status === 401) next.unauthorized = true;
            if (!next.error) next.error = result.reason.message;
          }
        });
        next.connected = successes === names.length;
        return next;
      });
      timer = setTimeout(load, 5000);
    }
    load();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [api, key, revision]);
  return state;
}

export function useExperiment(api, id, revision) {
  const [state, setState] = useState({ experiment: null, events: [], artifacts: [], plans: [], error: '', loading: true });
  const [refreshKey, setRefreshKey] = useState(0);
  const refresh = useCallback(() => setRefreshKey((key) => key + 1), []);
  const currentID = useRef(id);
  useEffect(() => {
    const controller = new AbortController();
    let timer;
    if (currentID.current !== id) {
      currentID.current = id;
      setState({ experiment: null, events: [], artifacts: [], plans: [], error: '', loading: true });
    }
    async function load() {
      const results = await Promise.allSettled([
        api.getExperiment(id, controller.signal), api.events(id, controller.signal),
        api.artifacts(id, controller.signal), api.plans(id, controller.signal),
      ]);
      if (controller.signal.aborted) return;
      setState((previous) => {
        const next = { ...previous, loading: false, error: '' };
        results.forEach((result, i) => {
          if (result.status === 'fulfilled') next[['experiment', 'events', 'artifacts', 'plans'][i]] = i === 0 ? result.value : result.value.data;
          else if (!next.error) next.error = result.reason.message;
        });
        return next;
      });
      timer = setTimeout(load, 2000);
    }
    load();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [api, id, revision, refreshKey]);
  const active = ['queued','running','planning'].includes(state.experiment?.status);
  useEffect(()=>{
    if(!active || !api.watchExperiment)return;
    const controller=new AbortController();
    api.watchExperiment(id, event=>{
      if(controller.signal.aborted||!event.id)return;
      setState(previous=>{const all=new Map(previous.events.map(item=>[item.id,item]));all.set(event.id,event);return {...previous,events:[...all.values()].sort((a,b)=>a.id-b.id).slice(-200)};});
      if(['completed','failed','cancelled','planned','planning_failed'].includes(event.kind))refresh();
    },controller.signal).catch(error=>{if(!controller.signal.aborted)setState(previous=>({...previous,error:'Live updates disconnected. Refreshing run status periodically.'}));});
    return ()=>controller.abort();
  },[api,id,active,refresh]);
  return { ...state, refresh };
}
