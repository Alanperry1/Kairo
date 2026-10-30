import React from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DatasetLibrary, { DatasetCandidateCard, maxDatasetBytes } from './DatasetLibrary';
afterEach(cleanup);
it('shows the 5 GB limit, real catalog results, and imports the chosen dataset',async()=>{
 const candidate={name:'Air quality',url:'https://example.com/data.csv',publisher:'Public catalog'};const api={searchDatasets:vi.fn().mockResolvedValue({workspace:[],public:[candidate]}),importDataset:vi.fn().mockResolvedValue({id:'ds_imported',name:'data.csv'})};const changed=vi.fn();
 render(<DatasetLibrary api={api} onChanged={changed}/>);expect(screen.getByText('CSV files up to 5 GB · stored in R2')).toBeTruthy();expect(maxDatasetBytes).toBe(5_000_000_000);
 await userEvent.type(screen.getByLabelText('Search public datasets'),'Air quality');await userEvent.click(screen.getByRole('button',{name:'Search'}));await userEvent.click(await screen.findByRole('button',{name:'Import to R2'}));expect(api.importDataset).toHaveBeenCalledWith({url:candidate.url,name:undefined},expect.any(AbortSignal));expect(changed).toHaveBeenCalled();expect(await screen.findByRole('button',{name:'Stored in R2'})).toBeTruthy();
});
it('rejects a file above 5 GB before sending it to the API',async()=>{
 const api={uploadDataset:vi.fn()};render(<DatasetLibrary api={api}/>);const file=new File(['x,y\n1,2\n'],'big.csv');Object.defineProperty(file,'size',{value:5_000_000_001});fireEvent.change(screen.getByLabelText('Upload dataset folder'),{target:{files:[file]}});expect((await screen.findByRole('alert')).textContent).toContain('exceeds the 5 GB limit');expect(api.uploadDataset).not.toHaveBeenCalled();
});
it('lets the user choose a workspace dataset without downloading it again',async()=>{
 const api={importDataset:vi.fn()};const use=vi.fn();render(<DatasetCandidateCard api={api} candidate={{name:'existing.csv',dataset_id:'ds_existing',publisher:'Your workspace'}} onUse={use}/>);await userEvent.click(screen.getByRole('button',{name:'Use dataset'}));expect(use).toHaveBeenCalledWith({id:'ds_existing',name:'existing.csv'});expect(api.importDataset).not.toHaveBeenCalled();
});
