import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface NetworkAddress {
  family: string;
  address: string;
  prefixlen: number;
  scope: string;
  broadcast: string | null;
}

interface NetworkInterfaceStats {
  rxBytes: number;
  txBytes: number;
  rxPackets: number;
  txPackets: number;
  rxErrors: number;
  txErrors: number;
  rxDropped: number;
  txDropped: number;
}

interface NetworkInterface {
  name: string;
  operstate: string;
  carrier: boolean;
  speed: number | null;
  duplex: string | null;
  mac: string;
  mtu: number;
  type: number;
  flags: string[];
  addresses: NetworkAddress[];
  statistics: NetworkInterfaceStats;
}

interface NetworkState {
  moduleId: string;
  timestamp: string;
  interfaces: NetworkInterface[];
}

interface NatRule {
  id: string;
  enabled: boolean;
  protocol: string;
  connectingDevices: string;
  incomingInterface: string;
  outgoingInterface: string;
  natAddr: string;
  originalPort: string;
  translatedPort: string;
  deviceAddr: string;
  deviceName: string;
  doubleNat: boolean;
  doubleNatAddr: string;
  comment: string;
}

interface NftablesConfig {
  natRules: NatRule[];
}

export const load: PageLoad = async ({ params }) => {
  if (params.serviceType === 'nftables') {
    return loadNftablesStatus();
  }
  return loadNetworkStatus();
};

async function loadNetworkStatus() {
  try {
    const result = await api<NetworkState>('/network/interfaces');

    return {
      networkState: result.data ?? null,
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      networkState: null,
      error: e instanceof Error ? e.message : 'Failed to load network state',
    };
  }
}

async function loadNftablesStatus() {
  try {
    const result = await api<NftablesConfig>('/nftables/config');

    return {
      nftablesConfig: result.data ?? { natRules: [] },
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      nftablesConfig: { natRules: [] },
      error: e instanceof Error ? e.message : 'Failed to load nftables status',
    };
  }
}
