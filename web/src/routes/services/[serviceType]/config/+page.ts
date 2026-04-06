import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface NetworkInterfaceConfig {
  interfaceName: string;
  dhcp4: boolean | null;
  addresses: string[] | null;
  gateway4: string | null;
  nameservers: string[] | null;
  mtu: number | null;
}

interface NetworkState {
  interfaces: {
    name: string;
    type: number;
    addresses: { family: string; address: string; prefixlen: number }[];
  }[];
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
    return loadNftablesConfig();
  }
  return loadNetworkConfig();
};

async function loadNetworkConfig() {
  try {
    const [configResult, interfacesResult] = await Promise.all([
      api<NetworkInterfaceConfig[]>('/network/config'),
      api<NetworkState>('/network/interfaces'),
    ]);

    // type 1 = ethernet (ARPHRD_ETHER), filter out loopback etc.
    const allNames = interfacesResult.data?.interfaces
      ?.filter((i) => i.type === 1)
      ?.map((i) => i.name) ?? [];

    return {
      configs: configResult.data ?? [],
      availableInterfaces: allNames,
      error: configResult.error?.error ?? interfacesResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      configs: [],
      availableInterfaces: [],
      error: e instanceof Error ? e.message : 'Failed to load network config',
    };
  }
}

async function loadNftablesConfig() {
  try {
    const [nftResult, interfacesResult] = await Promise.all([
      api<NftablesConfig>('/nftables/config'),
      api<NetworkState>('/network/interfaces'),
    ]);

    // type 1 = ethernet (ARPHRD_ETHER), filter out loopback etc.
    const ethInterfaces = interfacesResult.data?.interfaces
      ?.filter((i) => i.type === 1) ?? [];
    const allNames = ethInterfaces.map((i) => i.name);

    // Build a map of interface name -> primary IPv4 address for Double NAT placeholders
    const interfaceAddresses: Record<string, { address: string; prefixlen: number }> = {};
    for (const iface of ethInterfaces) {
      const ipv4 = iface.addresses?.find((a) => a.family === 'inet');
      if (ipv4) {
        interfaceAddresses[iface.name] = { address: ipv4.address, prefixlen: ipv4.prefixlen };
      }
    }

    return {
      nftablesConfig: nftResult.data ?? { natRules: [] },
      availableInterfaces: allNames,
      interfaceAddresses,
      error: nftResult.error?.error ?? interfacesResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      nftablesConfig: { natRules: [] },
      availableInterfaces: [],
      interfaceAddresses: {},
      error: e instanceof Error ? e.message : 'Failed to load nftables config',
    };
  }
}
