// The values the agent-cli plugin keeps in the session's $.state.
declare module 'claude-code' {
  interface PluginState {
    'agent-cli': {
      // The dispatch skill has loaded in this session, so its agent types are
      // offered to the model.
      dispatchLoaded: boolean
    }
  }
}
