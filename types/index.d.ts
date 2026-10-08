// The values the agentcli plugin keeps in the session's $.state.
declare module 'claude-code' {
  interface PluginState {
    'agentcli': {
      // The dispatch skill has loaded in this session, so its agent types are
      // offered to the model.
      dispatchLoaded: boolean
      // The band's lines: one for each run another caller started with
      // --agent-feedback that is still going, as the band shows it.
      hookRuns: { run_id: string; text: string }[]
    }
  }
}
