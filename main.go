package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"net"
	"runtime"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/pkg/skel"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

type SimpleBridge struct {
	BridgeName string `json:"bridgeName"`
	IP         string `json:"ip"`
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func createMacAddr() (string, error) {
	mac := make([]byte, 6)
	_, err := rand.Read(mac)
	if err != nil {
		fmt.Println("error:", err)
		return "", err
	}
	// Set the local bit
	mac[0] |= 2
	fmt.Printf("Random MAC address: %02x:%02x:%02x:%02x:%02x:%02x\n", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]), nil
	//return mac, nil
}

// Define your plugin's add function
func cmdAdd(args *skel.CmdArgs) error {
	log.Info("cmdAdd called with args: ", *args)

	sb := SimpleBridge{}
	if err := json.Unmarshal(args.StdinData, &sb); err != nil {
		return err
	}

	log.Info("Brige created ", sb.BridgeName, "\n", sb)

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: sb.BridgeName,
			MTU:  1500,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	err := netlink.LinkAdd(br)
	if err != nil && err != syscall.EEXIST {

		log.Error("err := netlink.LinkAdd(br)")
		return err
	}

	if err := netlink.LinkSetUp(br); err != nil {
		log.Error("if err := netlink.LinkSetUp(br); err != nil {", err)
		return err
	}

	//Create the veth
	l, err := netlink.LinkByName(sb.BridgeName)
	if err != nil {
		log.Error("could not lookup ", sb.BridgeName, ":", err)
		return err
	}

	newBr, ok := l.(*netlink.Bridge)
	if !ok {
		log.Info(sb.BridgeName, " already exists but is not a bridge")
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		log.Error(err)
		return err
	}

	hostIface := &cniv1.Interface{}
	mac, err := createMacAddr()
	if err != nil {
		log.Error("createMacAddr()", err)
		return err
	}
	var handler = func(hostNS ns.NetNS) error {
		hostVeth, containerVeth, err := ip.SetupVeth(args.IfName, 1500, mac, hostNS)
		if err != nil && err != syscall.EEXIST {
			log.Error("ip.SetupVeth", err)
			return err
		}

		hostIface.Name = hostVeth.Name

		ipv4Addr, ipv4Net, err := net.ParseCIDR(sb.IP)
		if err != nil {
			log.Error("net.ParseCIDR(sb.IP)", err)
			return err
		}

		link, err := netlink.LinkByName(containerVeth.Name)
		if err != nil {
			log.Error("netlink.LinkByName", err)
			return err
		}

		ipv4Net.IP = ipv4Addr

		addr := &netlink.Addr{IPNet: ipv4Net, Label: ""}
		if err = netlink.AddrAdd(link, addr); err != nil {
			log.Error("netlink.AddrAdd", err)
			return err
		}

		return nil
	}

	if err := netns.Do(handler); err != nil {
		log.Debug("Do.handler", err)
		return err
	}

	hostVeth, err := netlink.LinkByName(hostIface.Name)
	if err != nil {
		log.Error("netlink.LinkByName", err)
		return err
	}

	if err := netlink.LinkSetMaster(hostVeth, newBr); err != nil {
		log.Error("netlink.LinkSetMaster", err)
		return err
	}

	// Print the result in JSON format for the CNI runtime
	//return types.PrintResult(result, "1.0.0")
	return nil
}

// Define your plugin's check function
func cmdCheck(args *skel.CmdArgs) error {
	log.Info("cmdCheck called with args: ", args)
	return nil
}

// Define your plugin's delete function
func cmdDel(args *skel.CmdArgs) error {
	log.Info("cmdDel called with args: ", args)

	sb := SimpleBridge{}
	if err := json.Unmarshal(args.StdinData, &sb); err != nil {
		return err
	}

	log.Info("Brige created ", sb.BridgeName, "\n", sb)

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: sb.BridgeName,
			MTU:  1500,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	//Eliminamos los elementos en el container
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		log.Error(err)
		return err
	}

	var handler = func(hostNS ns.NetNS) error {

		err := ip.DelLinkByName(args.IfName)
		if err != nil && err != syscall.EEXIST {
			log.Error("netlink.DelLinkByName: ", err)
			return err
		}

		return nil
	}
	// Ejecutamos dentro del ns
	if err := netns.Do(handler); err != nil {
		log.Debug("Do.handler", err)
		return err
	}

	// Ponemos a DOWN el bridge en el host namespace
	if err := netlink.LinkSetDown(br); err != nil && err != syscall.EEXIST {
		log.Error("No se puede bajar el link: ", br.Name)
		return err
	}

	//Eliminamos el bridge en el host net namespace
	if err := netlink.LinkDel(br); err != nil {
		log.Error("No se puede eliminar el elemento: ", br.Name)
		return err
	}

	return nil
}

func main() {

	//f := skel.CNIFuncs{cmdAdd, cmdDel, cmdCheck, nil, nil}

	//skel.PluginMainFuncs(f, version.All, "My CNI plugin")

	// Register the command functions with skel.PluginMainFuncs
	log.Info("Entrando")
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "CNI Example Plugin")

}
