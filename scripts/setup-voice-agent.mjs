// Run with: node --env-file=.env scripts/setup-voice-agent.mjs
// The server SDK reads ELEVENLABS_API_KEY from the environment.
import { ElevenLabsClient } from '@elevenlabs/elevenlabs-js';
import { readFileSync, writeFileSync } from 'node:fs';
import { voiceAgentConfig } from '../config/voice-agent.mjs';

if (!process.env.ELEVENLABS_API_KEY) throw new Error('Complete ElevenLabs onboarding before setting up the voice agent.');
const client = new ElevenLabsClient();
try {
  let agentId = process.env.ELEVENLABS_AGENT_ID;
  if (agentId) {
    await client.conversationalAi.agents.get(agentId);
    console.log('Using the existing configured ElevenLabs agent.');
  } else {
    const agent = await client.conversationalAi.agents.create(voiceAgentConfig);
    agentId = agent.agentId;
    if (!agentId) throw new Error('Agent creation did not return an ID.');
    const path = new URL('../.env', import.meta.url);
    let text = readFileSync(path, 'utf8');
    const line = `ELEVENLABS_AGENT_ID=${agentId}`;
    text = /^ELEVENLABS_AGENT_ID=/m.test(text) ? text.replace(/^ELEVENLABS_AGENT_ID=.*$/m, line) : `${text}\n${line}\n`;
    writeFileSync(path, text, { mode: 0o600 });
    console.log('Created Kairo voice agent and saved its ID in the shared root .env.');
  }
  console.log(`Agent: ${agentId}`);
} catch (error) {
  // Do not print request headers, environment values, or SDK request objects.
  console.error(`ElevenLabs agent setup failed${error.statusCode ? ` (HTTP ${error.statusCode})` : ''}. Check workspace access and the agent configuration.`);
  process.exitCode = 1;
}
